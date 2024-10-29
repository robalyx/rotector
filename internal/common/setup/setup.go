package setup

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaxron/axonet/middleware/circuitbreaker"
	"github.com/jaxron/axonet/middleware/proxy"
	"github.com/jaxron/axonet/middleware/ratelimit"
	"github.com/jaxron/axonet/middleware/redis"
	"github.com/jaxron/axonet/middleware/retry"
	"github.com/jaxron/axonet/middleware/singleflight"
	"github.com/jaxron/axonet/pkg/client"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/logging"
	"github.com/rotector/rotector/internal/common/statistics"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// AppSetup contains all the common setup components.
type AppSetup struct {
	Config       *config.Config
	Logger       *zap.Logger
	DBLogger     *zap.Logger
	DB           *database.Database
	RedisClient  rueidis.Client
	OpenAIClient *openai.Client
	RoAPI        *api.API
}

// InitializeApp performs common setup tasks and returns an AppSetup.
func InitializeApp(logDir string) (*AppSetup, error) {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Initialize logging
	logger, dbLogger, err := logging.SetupLogging(logDir, cfg.Logging.Level, cfg.Logging.MaxLogsToKeep)
	if err != nil {
		return nil, err
	}

	// Initialize Redis client
	redisClient, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)},
		Username:    cfg.Redis.Username,
		Password:    cfg.Redis.Password,
		SelectDB:    statistics.DBIndex,
	})
	if err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
		return nil, err
	}

	// Initialize Statistics
	stats := statistics.NewStatistics(redisClient)

	// Initialize database connection
	db, err := database.NewConnection(cfg, stats, dbLogger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
		return nil, err
	}

	// Initialize OpenAI client
	openaiClient := openai.NewClient(
		option.WithAPIKey(cfg.OpenAI.APIKey),
	)

	// Initialize AppSetup
	setup := &AppSetup{
		Config:       cfg,
		Logger:       logger,
		DBLogger:     dbLogger,
		DB:           db,
		RedisClient:  redisClient,
		OpenAIClient: openaiClient,
	}

	// Initialize RoAPI client
	roAPI, err := setup.getRoAPIClient()
	if err != nil {
		logger.Fatal("failed to create RoAPI client", zap.Error(err))
		return nil, err
	}
	setup.RoAPI = roAPI

	return setup, nil
}

// CleanupApp performs cleanup tasks.
func (s *AppSetup) CleanupApp() {
	if err := s.Logger.Sync(); err != nil {
		log.Printf("Failed to sync logger: %v", err)
	}
	if err := s.DBLogger.Sync(); err != nil {
		log.Printf("Failed to sync DB logger: %v", err)
	}
	if err := s.DB.Close(); err != nil {
		log.Printf("Failed to close database connection: %v", err)
	}
	s.RedisClient.Close()
}

// getRoAPIClient creates a new RoAPI client with the given configuration.
func (s *AppSetup) getRoAPIClient() (*api.API, error) {
	// Read the cookies and proxies
	cookies := s.readCookies()
	proxies := s.readProxies()

	// Initialize Redis cache
	redisClient, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{fmt.Sprintf("%s:%d", s.Config.Redis.Host, s.Config.Redis.Port)},
		Username:    s.Config.Redis.Username,
		Password:    s.Config.Redis.Password,
	})
	if err != nil {
		return nil, err
	}

	return api.New(cookies,
		client.WithLogger(NewLogger(s.Logger)),
		client.WithTimeout(10*time.Second),
		client.WithMiddleware(6, circuitbreaker.New(s.Config.CircuitBreaker.MaxFailures, s.Config.CircuitBreaker.FailureThreshold, s.Config.CircuitBreaker.RecoveryTimeout)),
		client.WithMiddleware(5, retry.New(5, 500*time.Millisecond, 1000*time.Millisecond)),
		client.WithMiddleware(4, singleflight.New()),
		client.WithMiddleware(3, redis.New(redisClient, 1*time.Hour)),
		client.WithMiddleware(2, ratelimit.New(s.Config.RateLimit.RequestsPerSecond, s.Config.RateLimit.BurstSize)),
		client.WithMiddleware(1, proxy.New(proxies)),
	), nil
}

// readProxies reads the proxies from the given configuration file.
func (s *AppSetup) readProxies() []*url.URL {
	var proxies []*url.URL

	// Open the file
	proxiesFile := filepath.Dir(viper.GetViper().ConfigFileUsed()) + "/credentials/proxies"
	file, err := os.Open(proxiesFile)
	if err != nil {
		s.Logger.Fatal("failed to open proxy file", zap.Error(err))
		return nil
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Split the line into parts (IP:Port:Username:Password)
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) != 4 {
			s.Logger.Fatal("invalid proxy format", zap.String("proxy", scanner.Text()))
			return nil
		}

		// Extract proxy components
		ip := parts[0]
		port := parts[1]
		username := parts[2]
		password := parts[3]

		// Construct the proxy URL
		proxyURL := fmt.Sprintf("http://%s:%s@%s", username, password, net.JoinHostPort(ip, port))

		// Parse the proxy URL
		parsedURL, err := url.Parse(proxyURL)
		if err != nil {
			s.Logger.Fatal("failed to parse proxy URL", zap.Error(err))
			return nil
		}

		// Add the proxy to the list
		proxies = append(proxies, parsedURL)
	}

	// Check for any errors during scanning
	if err := scanner.Err(); err != nil {
		s.Logger.Fatal("error reading proxy file", zap.Error(err))
		return nil
	}

	return proxies
}

// readCookies reads the cookies from the given configuration file.
func (s *AppSetup) readCookies() []string {
	var cookies []string

	// Open the file
	cookiesFile := filepath.Dir(viper.GetViper().ConfigFileUsed()) + "/credentials/cookies"
	file, err := os.Open(cookiesFile)
	if err != nil {
		s.Logger.Fatal("failed to open cookie file", zap.Error(err))
		return nil
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		cookies = append(cookies, scanner.Text())
	}

	// Check for any errors during scanning
	if err := scanner.Err(); err != nil {
		s.Logger.Fatal("error reading cookie file", zap.Error(err))
		return nil
	}

	return cookies
}
