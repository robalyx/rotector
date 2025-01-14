package client

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/jaxron/axonet/middleware/circuitbreaker"
	axonetRedis "github.com/jaxron/axonet/middleware/redis"
	"github.com/jaxron/axonet/middleware/retry"
	"github.com/jaxron/axonet/middleware/singleflight"
	"github.com/jaxron/axonet/pkg/client"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/common/setup/client/middleware/proxy"
	"github.com/robalyx/rotector/internal/common/setup/config"
	"github.com/robalyx/rotector/internal/common/setup/logger"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"go.uber.org/zap"
)

// GetRoAPIClient constructs an HTTP client with a middleware chain for reliability and performance.
// Middleware order is important - each layer wraps the next in specified priority.
func GetRoAPIClient(cfg *config.CommonConfig, configDir string, redisManager *redis.Manager, zapLogger *zap.Logger) (*api.API, *proxy.Proxies, error) {
	// Load authentication and proxy configuration
	cookies := readCookies(configDir, zapLogger)
	proxies := readProxies(configDir, zapLogger)

	// Get Redis client for caching
	redisClient, err := redisManager.GetClient(redis.CacheDBIndex)
	if err != nil {
		return nil, nil, err
	}

	// Get Redis client for proxy rotation
	proxyClient, err := redisManager.GetClient(redis.ProxyDBIndex)
	if err != nil {
		return nil, nil, err
	}

	// Initialize proxy middleware
	proxyMiddleware := proxy.New(proxies, proxyClient, &cfg.Proxy)

	// Build client with middleware chain in priority order:
	// 5. Circuit breaker prevents cascading failures
	// 4. Retry handles transient failures
	// 3. Single flight deduplicates concurrent requests
	// 2. Redis caching reduces API load
	// 1. Proxy routing with rate limiting
	return api.New(cookies,
		client.WithMarshalFunc(sonic.Marshal),
		client.WithUnmarshalFunc(sonic.Unmarshal),
		client.WithLogger(logger.New(zapLogger)),
		client.WithTimeout(time.Duration(cfg.Proxy.RequestTimeout)*time.Millisecond),
		client.WithMiddleware(
			circuitbreaker.New(
				cfg.CircuitBreaker.MaxFailures,
				time.Duration(cfg.CircuitBreaker.FailureThreshold)*time.Millisecond,
				time.Duration(cfg.CircuitBreaker.RecoveryTimeout)*time.Millisecond,
			),
		),
		client.WithMiddleware(
			retry.New(
				cfg.Retry.MaxRetries,
				time.Duration(cfg.Retry.Delay)*time.Millisecond,
				time.Duration(cfg.Retry.MaxDelay)*time.Millisecond,
			),
		),
		client.WithMiddleware(singleflight.New()),
		client.WithMiddleware(axonetRedis.New(redisClient, 1*time.Hour)),
		client.WithMiddleware(proxyMiddleware),
	), proxyMiddleware, nil
}

// readProxies parses proxy configuration from a file in IP:Port:Username:Password format.
// Each line represents one proxy server. Invalid formats trigger fatal errors.
func readProxies(configDir string, logger *zap.Logger) []*url.URL {
	var proxies []*url.URL

	// Load proxy configuration file
	proxiesFile := configDir + "/credentials/proxies"
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
func readCookies(configDir string, logger *zap.Logger) []string {
	var cookies []string

	// Load cookie file
	cookiesFile := configDir + "/credentials/cookies"
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
