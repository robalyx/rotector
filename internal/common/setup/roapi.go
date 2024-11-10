package setup

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/jaxron/axonet/middleware/circuitbreaker"
	"github.com/jaxron/axonet/middleware/proxy"
	"github.com/jaxron/axonet/middleware/ratelimit"
	axonetRedis "github.com/jaxron/axonet/middleware/redis"
	"github.com/jaxron/axonet/middleware/retry"
	"github.com/jaxron/axonet/middleware/singleflight"
	"github.com/jaxron/axonet/pkg/client"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/redis"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

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
		client.WithMarshalFunc(sonic.Marshal),
		client.WithUnmarshalFunc(sonic.Unmarshal),
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
