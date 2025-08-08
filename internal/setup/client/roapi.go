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
	"github.com/robalyx/rotector/internal/redis"
	"github.com/robalyx/rotector/internal/setup/client/interceptor/proxy"
	"github.com/robalyx/rotector/internal/setup/client/interceptor/roverse"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/internal/setup/telemetry/logger"
	"go.uber.org/zap"
)

var (
	ErrInvalidProxyFormat = errors.New("invalid proxy format")
	ErrNoProxies          = errors.New("no valid proxies found in proxy file")
	ErrNoCookies          = errors.New("no valid cookies found in cookie file")
)

// Middlewares contains the middleware instances used in the client.
type Middlewares struct {
	Proxy   *proxy.Middleware
	Roverse *roverse.Middleware
}

// GetRoAPIClient constructs an HTTP client with a middleware chain for reliability and performance.
// Middleware order is important - each layer wraps the next in specified priority.
func GetRoAPIClient(
	cfg *config.CommonConfig, configDir string, redisManager *redis.Manager,
	zapLogger *zap.Logger, requestTimeout time.Duration,
) (*api.API, *Middlewares, error) {
	// Load authentication and proxy configuration
	cookies, err := readCookies(configDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read cookies: %w", err)
	}

	// Load regular proxies
	proxiesPath := configDir + "/credentials/regular_proxies"

	proxies, err := readProxiesFromFile(proxiesPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read proxies: %w", err)
	}

	// Load Roverse-specific proxies if the file exists
	roverseProxiesPath := configDir + "/credentials/roverse_proxies"

	roverseProxies, err := readProxiesFromFile(roverseProxiesPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, ErrNoProxies) {
		return nil, nil, fmt.Errorf("failed to read roverse proxies: %w", err)
	}

	// Merge roverse proxies into regular proxies if they don't already exist
	if len(roverseProxies) > 0 {
		// Create a map for quick lookup of regular proxy URLs
		proxyMap := make(map[string]struct{}, len(proxies))
		for _, proxyURL := range proxies {
			proxyMap[proxyURL.String()] = struct{}{}
		}

		// Add roverse proxies that don't already exist in the regular proxies list
		added := 0

		for _, roverseProxy := range roverseProxies {
			if _, exists := proxyMap[roverseProxy.String()]; !exists {
				proxies = append(proxies, roverseProxy)
				added++
			}
		}

		zapLogger.Debug("Added roverse proxies to regular proxies list", zap.Int("count", added))
	}

	// Get Redis client for caching
	redisClient, err := redisManager.GetClient(redis.CacheDBIndex)
	if err != nil {
		return nil, nil, err
	}

	// Get Redis client for proxy rotation
	ratelimitClient, err := redisManager.GetClient(redis.RatelimitDBIndex)
	if err != nil {
		return nil, nil, err
	}

	// Initialize middleware instances
	proxyMiddleware := proxy.New(proxies, ratelimitClient, cfg, requestTimeout)
	roverseMiddleware := roverse.New(roverseProxies, ratelimitClient, cfg, requestTimeout)

	// Build middleware chain - order matters!
	middlewares := []middleware.Middleware{
		circuitbreaker.New(
			cfg.CircuitBreaker.MaxRequests,
			time.Duration(cfg.CircuitBreaker.Interval)*time.Millisecond,
			time.Duration(cfg.CircuitBreaker.Timeout)*time.Millisecond,
		),
		retry.New(
			cfg.Retry.MaxRetries,
			time.Duration(cfg.Retry.Delay)*time.Millisecond,
			time.Duration(cfg.Retry.MaxDelay)*time.Millisecond,
		),
		singleflight.New(),
		axonetRedis.New(redisClient, 1*time.Hour),
		proxyMiddleware,
		roverseMiddleware,
	}

	// Create middleware wrapper
	middlewareWrapper := &Middlewares{
		Proxy:   proxyMiddleware,
		Roverse: roverseMiddleware,
	}

	return api.New(cookies,
		client.WithMarshalFunc(sonic.Marshal),
		client.WithUnmarshalFunc(sonic.Unmarshal),
		client.WithLogger(logger.New(zapLogger)),
		client.WithTimeout(requestTimeout),
		client.WithMiddleware(middlewares...),
	), middlewareWrapper, nil
}

// readProxiesFromFile loads proxy configuration from a file in IP:Port:Username:Password format.
// Returns error if no valid proxies are found or if the file doesn't exist.
func readProxiesFromFile(filePath string) ([]*url.URL, error) {
	var proxies []*url.URL

	// Load proxy configuration file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Process each proxy line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split the line into parts (IP:Port:Username:Password)
		parts := strings.Split(line, ":")
		if len(parts) != 4 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidProxyFormat, line)
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
