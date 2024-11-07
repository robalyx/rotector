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
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/redis"
	"github.com/rotector/rotector/internal/common/statistics"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// AppSetup bundles all core dependencies and services needed by the application.
// Each field represents a major subsystem that needs initialization and cleanup.
type AppSetup struct {
	Config       *config.Config     // Application configuration
	Logger       *zap.Logger        // Main application logger
	DBLogger     *zap.Logger        // Database-specific logger
	DB           *database.Database // Database connection pool
	OpenAIClient *openai.Client     // OpenAI API client
	Stats        *statistics.Client // Statistics tracking
	RoAPI        *api.API           // RoAPI HTTP client
	Queue        *queue.Manager     // Background job queue
	RedisManager *redis.Manager     // Redis connection manager
	LogManager   *LogManager        // Log management system
}

// InitializeApp bootstraps all application dependencies in the correct order,
// ensuring each component has its required dependencies available.
func InitializeApp(logDir string) (*AppSetup, error) {
	// Configuration must be loaded first as other components depend on it
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Logging system is initialized next to capture setup issues
	logManager := NewLogManager(logDir, cfg.Logging.Level, cfg.Logging.MaxLogsToKeep)
	logger, dbLogger, err := logManager.GetLoggers()
	if err != nil {
		return nil, err
	}

	// Redis manager provides connection pools for various subsystems
	redisManager := redis.NewManager(cfg, logger)

	// Statistics tracking requires its own Redis database
	statsRedis, err := redisManager.GetClient(redis.StatsDBIndex)
	if err != nil {
		logger.Fatal("Failed to create statistics Redis client", zap.Error(err))
		return nil, err
	}
	stats := statistics.NewClient(statsRedis, logger)

	// Database connection pool is created with statistics tracking
	db, err := database.NewConnection(cfg, stats, dbLogger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
		return nil, err
	}

	// OpenAI client is configured with API key from config
	openaiClient := openai.NewClient(
		option.WithAPIKey(cfg.OpenAI.APIKey),
		option.WithRequestTimeout(30*time.Second),
	)

	// RoAPI client is configured with middleware chain
	roAPI, err := getRoAPIClient(cfg, redisManager, logger)
	if err != nil {
		logger.Fatal("Failed to create RoAPI client", zap.Error(err))
		return nil, err
	}

	// Queue manager creates its own Redis database for job storage
	queueRedis, err := redisManager.GetClient(redis.QueueDBIndex)
	if err != nil {
		logger.Fatal("Failed to create queue Redis client", zap.Error(err))
		return nil, err
	}
	queueManager := queue.NewManager(db, queueRedis, logger)

	// Bundle all initialized components
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
		LogManager:   logManager,
	}, nil
}

// CleanupApp ensures graceful shutdown of all components in reverse initialization order.
// Logs but does not fail on cleanup errors to ensure all components get cleanup attempts.
func (s *AppSetup) CleanupApp() {
	// Sync buffered logs before shutdown
	if err := s.Logger.Sync(); err != nil {
		log.Printf("Failed to sync logger: %v", err)
	}
	if err := s.DBLogger.Sync(); err != nil {
		log.Printf("Failed to sync DB logger: %v", err)
	}

	// Close database connections
	if err := s.DB.Close(); err != nil {
		log.Printf("Failed to close database connection: %v", err)
	}

	// Close Redis connections last as other components might need it during cleanup
	s.RedisManager.Close()
}

// getRoAPIClient constructs an HTTP client with a middleware chain for reliability and performance.
// Middleware order is important - each layer wraps the next in specified priority.
func getRoAPIClient(cfg *config.Config, redisManager *redis.Manager, logger *zap.Logger) (*api.API, error) {
	// Load authentication and proxy configuration
	cookies := readCookies(logger)
	proxies := readProxies(logger)

	// Cache layer requires its own Redis database
	redisClient, err := redisManager.GetClient(redis.CacheDBIndex)
	if err != nil {
		return nil, err
	}

	// Build client with middleware chain in priority order:
	// 6. Circuit breaker prevents cascading failures
	// 5. Retry handles transient failures
	// 4. Single flight deduplicates concurrent requests
	// 3. Redis caching reduces API load
	// 2. Rate limiting prevents API abuse
	// 1. Proxy routing distributes requests
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
		client.WithMiddleware(5, retry.New(5, 1*time.Second, 5*time.Second)),
		client.WithMiddleware(4, singleflight.New()),
		client.WithMiddleware(3, axonetRedis.New(redisClient, 1*time.Hour)),
		client.WithMiddleware(2, ratelimit.New(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.BurstSize)),
		client.WithMiddleware(1, proxy.New(proxies)),
	), nil
}

// readProxies parses proxy configuration from a file in IP:Port:Username:Password format.
// Each line represents one proxy server. Invalid formats trigger fatal errors.
func readProxies(logger *zap.Logger) []*url.URL {
	var proxies []*url.URL

	// Load proxy configuration file
	proxiesFile := filepath.Dir(viper.GetViper().ConfigFileUsed()) + "/credentials/proxies"
	file, err := os.Open(proxiesFile)
	if err != nil {
		logger.Fatal("failed to open proxy file", zap.Error(err))
		return nil
	}
	defer file.Close()

	// Process each proxy line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Split the line into parts (IP:Port:Username:Password)
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) != 4 {
			logger.Fatal("invalid proxy format", zap.String("proxy", scanner.Text()))
			return nil
		}

		// Build proxy URL with authentication
		ip, port, username, password := parts[0], parts[1], parts[2], parts[3]
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

// readCookies loads authentication cookies from a file, one cookie per line.
// Returns empty slice and logs error if file cannot be read.
func readCookies(logger *zap.Logger) []string {
	var cookies []string

	// Load cookie file
	cookiesFile := filepath.Dir(viper.GetViper().ConfigFileUsed()) + "/credentials/cookies"
	file, err := os.Open(cookiesFile)
	if err != nil {
		logger.Fatal("failed to open cookie file", zap.Error(err))
		return nil
	}
	defer file.Close()

	// Read cookies line by line
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
