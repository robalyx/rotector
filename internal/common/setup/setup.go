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
	axonetRedis "github.com/jaxron/axonet/middleware/redis"
	"github.com/jaxron/axonet/middleware/retry"
	"github.com/jaxron/axonet/middleware/singleflight"
	"github.com/jaxron/axonet/pkg/client"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/logging"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/redis"
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
	OpenAIClient *openai.Client
	Stats        *statistics.Statistics
	RoAPI        *api.API
	Queue        *queue.Manager
	RedisManager *redis.Manager
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

	// Initialize Redis manager
	redisManager := redis.NewManager(cfg, logger)

	// Initialize statistics client
	statsRedis, err := redisManager.GetClient(redis.StatsDBIndex)
	if err != nil {
		logger.Fatal("Failed to create statistics Redis client", zap.Error(err))
		return nil, err
	}
	stats := statistics.NewStatistics(statsRedis, logger)

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

	// Initialize RoAPI client
	roAPI, err := getRoAPIClient(cfg, redisManager, logger)
	if err != nil {
		logger.Fatal("Failed to create RoAPI client", zap.Error(err))
		return nil, err
	}

	// Initialize Queue manager
	queueRedis, err := redisManager.GetClient(redis.QueueDBIndex)
	if err != nil {
		logger.Fatal("Failed to create queue Redis client", zap.Error(err))
		return nil, err
	}
	queueManager := queue.NewManager(db, queueRedis, logger)

	// Initialize AppSetup
	return &AppSetup{
		Config:       cfg,
		Logger:       logger,
		DBLogger:     dbLogger,
		DB:           db,
		OpenAIClient: openaiClient,
		Stats:        stats,
		RoAPI:        roAPI,
		Queue:        queueManager,
		RedisManager: redisManager,
	}, nil
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
	s.RedisManager.Close()
}

// getRoAPIClient creates a new RoAPI client with the given configuration.
func getRoAPIClient(cfg *config.Config, redisManager *redis.Manager, logger *zap.Logger) (*api.API, error) {
	// Read the cookies and proxies
	cookies := readCookies(logger)
	proxies := readProxies(logger)

	// Initialize Redis cache
	redisClient, err := redisManager.GetClient(redis.CacheDBIndex)
	if err != nil {
		return nil, err
	}

	return api.New(cookies,
		client.WithLogger(NewLogger(logger)),
		client.WithTimeout(10*time.Second),
		client.WithMiddleware(6,
			circuitbreaker.New(
				cfg.CircuitBreaker.MaxFailures,
				cfg.CircuitBreaker.FailureThreshold,
				cfg.CircuitBreaker.RecoveryTimeout,
			),
		),
		client.WithMiddleware(5, retry.New(5, 500*time.Millisecond, 1000*time.Millisecond)),
		client.WithMiddleware(4, singleflight.New()),
		client.WithMiddleware(3, axonetRedis.New(redisClient, 1*time.Hour)),
		client.WithMiddleware(2, ratelimit.New(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.BurstSize)),
		client.WithMiddleware(1, proxy.New(proxies)),
	), nil
}

// readProxies reads the proxies from the given configuration file.
func readProxies(logger *zap.Logger) []*url.URL {
	var proxies []*url.URL

	// Open the file
	proxiesFile := filepath.Dir(viper.GetViper().ConfigFileUsed()) + "/credentials/proxies"
	file, err := os.Open(proxiesFile)
	if err != nil {
		logger.Fatal("failed to open proxy file", zap.Error(err))
		return nil
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Split the line into parts (IP:Port:Username:Password)
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) != 4 {
			logger.Fatal("invalid proxy format", zap.String("proxy", scanner.Text()))
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
			logger.Fatal("failed to parse proxy URL", zap.Error(err))
			return nil
		}

		// Add the proxy to the list
		proxies = append(proxies, parsedURL)
	}

	// Check for any errors during scanning
	if err := scanner.Err(); err != nil {
		logger.Fatal("error reading proxy file", zap.Error(err))
		return nil
	}

	return proxies
}

// readCookies reads the cookies from the given configuration file.
func readCookies(logger *zap.Logger) []string {
	var cookies []string

	// Open the file
	cookiesFile := filepath.Dir(viper.GetViper().ConfigFileUsed()) + "/credentials/cookies"
	file, err := os.Open(cookiesFile)
	if err != nil {
		logger.Fatal("failed to open cookie file", zap.Error(err))
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
		logger.Fatal("error reading cookie file", zap.Error(err))
		return nil
	}

	return cookies
}
