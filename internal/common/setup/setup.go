package setup

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jaxron/axonet/middleware/circuitbreaker"
	"github.com/jaxron/axonet/middleware/proxy"
	"github.com/jaxron/axonet/middleware/ratelimit"
	"github.com/jaxron/axonet/middleware/rediscache"
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
	setup.RoAPI = setup.getRoAPIClient()

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
func (s *AppSetup) getRoAPIClient() *api.API {
	// Read the cookies and proxies
	cookies := s.readCookies()
	proxies := s.readProxies()

	// Initialize Redis cache
	cache, err := rediscache.New(rueidis.ClientOption{
		InitAddress: []string{fmt.Sprintf("%s:%d", s.Config.Redis.Host, s.Config.Redis.Port)},
		Username:    s.Config.Redis.Username,
		Password:    s.Config.Redis.Password,
	}, 5*time.Minute)
	if err != nil {
		s.Logger.Fatal("failed to create Redis cache", zap.Error(err))
		return nil
	}

	return api.New(cookies,
		client.WithLogger(NewLogger(s.Logger)),
		client.WithTimeout(20*time.Second),
		client.WithMiddleware(cache),
		client.WithMiddleware(circuitbreaker.New(s.Config.CircuitBreaker.MaxFailures, s.Config.CircuitBreaker.FailureThreshold, s.Config.CircuitBreaker.RecoveryTimeout)),
		client.WithMiddleware(ratelimit.New(s.Config.RateLimit.RequestsPerSecond, s.Config.RateLimit.BurstSize)),
		client.WithMiddleware(retry.New(5, 500*time.Millisecond, 1000*time.Millisecond)),
		client.WithMiddleware(singleflight.New()),
		client.WithMiddleware(proxy.New(proxies)),
	)
}

// readProxies reads the proxies from the given configuration file.
func (s *AppSetup) readProxies() []*url.URL {
	// If no proxies file is set, return an empty list
	if s.Config.Roblox.ProxiesFile == "" {
		s.Logger.Warn("No proxies file set")
		return []*url.URL{}
	}

	var proxies []*url.URL

	// Open the file
	file, err := os.Open(s.Config.Roblox.ProxiesFile)
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
	// If no cookies file is set, return an empty list
	if s.Config.Roblox.CookiesFile == "" {
		s.Logger.Warn("No cookies file set")
		return []string{}
	}

	var cookies []string

	// Open the file
	file, err := os.Open(s.Config.Roblox.CookiesFile)
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
