package client

import (
	"bufio"
	"errors"
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
	"github.com/jaxron/axonet/pkg/client/middleware"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/common/setup/client/middleware/proxy"
	"github.com/robalyx/rotector/internal/common/setup/config"
	"github.com/robalyx/rotector/internal/common/setup/logger"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"go.uber.org/zap"
)

var (
	ErrInvalidProxyFormat = errors.New("invalid proxy format")
	ErrNoProxies          = errors.New("no valid proxies found in proxy file")
	ErrNoCookies          = errors.New("no valid cookies found in cookie file")
)

// GetRoAPIClient constructs an HTTP client with a middleware chain for reliability and performance.
// Middleware order is important - each layer wraps the next in specified priority.
func GetRoAPIClient(cfg *config.CommonConfig, configDir string, redisManager *redis.Manager, zapLogger *zap.Logger, requestTimeout time.Duration) (*api.API, *proxy.Proxies, error) {
	// Load authentication and proxy configuration
	cookies, err := readCookies(configDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read cookies: %w", err)
	}

	proxies, err := readProxies(configDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read proxies: %w", err)
	}

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

	// Build middleware chain
	proxyMiddleware := proxy.New(proxies, proxyClient, cfg, requestTimeout)
	middlewares := []middleware.Middleware{
		circuitbreaker.New(
			cfg.CircuitBreaker.MaxFailures,
			time.Duration(cfg.CircuitBreaker.FailureThreshold)*time.Millisecond,
			time.Duration(cfg.CircuitBreaker.RecoveryTimeout)*time.Millisecond,
		),
		retry.New(
			cfg.Retry.MaxRetries,
			time.Duration(cfg.Retry.Delay)*time.Millisecond,
			time.Duration(cfg.Retry.MaxDelay)*time.Millisecond,
		),
		singleflight.New(),
		axonetRedis.New(redisClient, 1*time.Hour),
		proxyMiddleware,
	}

	return api.New(cookies,
		client.WithMarshalFunc(sonic.Marshal),
		client.WithUnmarshalFunc(sonic.Unmarshal),
		client.WithLogger(logger.New(zapLogger)),
		client.WithTimeout(requestTimeout),
		client.WithMiddleware(middlewares...),
	), proxyMiddleware, nil
}

// readProxies parses proxy configuration from a file in IP:Port:Username:Password format.
// Returns error if no valid proxies are found.
func readProxies(configDir string) ([]*url.URL, error) {
	var proxies []*url.URL

	// Load proxy configuration file
	proxiesFile := configDir + "/credentials/proxies"
	file, err := os.Open(proxiesFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open proxy file: %w", err)
	}
	defer file.Close()

	// Process each proxy line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Split the line into parts (IP:Port:Username:Password)
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) != 4 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidProxyFormat, scanner.Text())
		}

		// Build proxy URL with authentication
		ip, port, username, password := parts[0], parts[1], parts[2], parts[3]
		proxyURL := fmt.Sprintf("http://%s:%s@%s", username, password, net.JoinHostPort(ip, port))

		// Parse the proxy URL
		parsedURL, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse proxy URL: %w", err)
		}

		// Add the proxy to the list
		proxies = append(proxies, parsedURL)
	}

	// Check for any errors during scanning
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading proxy file: %w", err)
	}

	if len(proxies) == 0 {
		return nil, ErrNoProxies
	}

	return proxies, nil
}

// readCookies loads authentication cookies from a file, one cookie per line.
// Returns error if no valid cookies are found.
func readCookies(configDir string) ([]string, error) {
	var cookies []string

	// Load cookie file
	cookiesFile := configDir + "/credentials/cookies"
	file, err := os.Open(cookiesFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open cookie file: %w", err)
	}
	defer file.Close()

	// Read cookies line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		cookie := strings.TrimSpace(scanner.Text())
		if cookie != "" {
			cookies = append(cookies, cookie)
		}
	}

	// Check for any errors during scanning
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading cookie file: %w", err)
	}

	if len(cookies) == 0 {
		return nil, ErrNoCookies
	}

	return cookies, nil
}
