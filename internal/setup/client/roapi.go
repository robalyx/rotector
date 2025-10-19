package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
	ErrNoProxies            = errors.New("no valid proxies found in proxy file")
	ErrNoCookies            = errors.New("no valid cookies found in cookie file")
	ErrAPIFetchFailed       = errors.New("failed to fetch proxies from API")
	ErrInvalidProxyResponse = errors.New("invalid proxy response format")
	ErrMissingProxyConfig   = errors.New("missing proxy API configuration")
)

// HTTP client with timeout for proxy API calls.
var proxyAPIClient = &http.Client{
	Timeout: 30 * time.Second,
}

// Middlewares contains the middleware instances used in the client.
type Middlewares struct {
	Proxy   *proxy.Middleware
	Roverse *roverse.Middleware
}

// GetRoAPIClient constructs an HTTP client with a middleware chain for reliability and performance.
func GetRoAPIClient(
	ctx context.Context, cfg *config.CommonConfig, configDir string, redisManager *redis.Manager,
	zapLogger *zap.Logger, requestTimeout time.Duration,
) (*api.API, *Middlewares, error) {
	// Load authentication and proxy configuration
	cookies, err := readCookies(configDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read cookies: %w", err)
	}

	// Validate proxy API configuration
	if cfg.Proxy.ProxyAPIURL == "" || cfg.Proxy.ProxyAPIKey == "" {
		return nil, nil, fmt.Errorf("%w: ProxyAPIURL and ProxyAPIKey must be set", ErrMissingProxyConfig)
	}

	// Load regular proxies from API
	proxies, err := fetchRegularProxiesFromAPI(ctx, cfg.Proxy.ProxyAPIURL, cfg.Proxy.ProxyAPIKey, zapLogger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch proxies from API: %w", err)
	}

	// Load proxies for roverse from API if configured
	var roverseProxies []*url.URL
	if cfg.Roverse.ProxyAPIURL != "" && cfg.Roverse.ProxyAPIKey != "" {
		roverseProxies, err = fetchRoverseProxiesFromAPI(ctx, cfg.Roverse.ProxyAPIURL, cfg.Roverse.ProxyAPIKey, zapLogger)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch roverse proxies from API: %w", err)
		}
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

// fetchRoverseProxiesFromAPI fetches roverse proxy list from configured API endpoint.
// Returns proxies in format http://username:password@IP:Port with authentication credentials.
func fetchRoverseProxiesFromAPI(ctx context.Context, baseURL, apiKey string, zapLogger *zap.Logger) ([]*url.URL, error) {
	zapLogger.Debug("Fetching roverse proxies from API", zap.String("url", baseURL))

	var allProxies []*url.URL

	page := 1
	pageSize := 100

	for {
		// Build URL with query parameters
		params := url.Values{}
		params.Add("mode", "direct")
		params.Add("page", strconv.Itoa(page))
		params.Add("page_size", strconv.Itoa(pageSize))

		fullURL := baseURL + "?" + params.Encode()

		// Create HTTP request with Token authentication
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to create request: %w", ErrAPIFetchFailed, err)
		}

		req.Header.Set("Authorization", "Token "+apiKey)

		// Make HTTP GET request
		resp, err := proxyAPIClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAPIFetchFailed, err)
		}
		defer resp.Body.Close()

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read response body: %w", ErrAPIFetchFailed, err)
		}

		// Check status code
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%w: HTTP %d: %s", ErrAPIFetchFailed, resp.StatusCode, string(body))
		}

		// Parse JSON response
		//nolint:tagliatelle // API returns snake_case, not camelCase
		var apiResponse struct {
			Count    int    `json:"count"`
			Next     string `json:"next"`
			Previous string `json:"previous"`
			Results  []struct {
				ID               string `json:"id"`
				Username         string `json:"username"`
				Password         string `json:"password"`
				ProxyAddress     string `json:"proxy_address"`
				Port             int    `json:"port"`
				Valid            bool   `json:"valid"`
				LastVerification string `json:"last_verification"`
				CountryCode      string `json:"country_code"`
				CityName         string `json:"city_name"`
				CreatedAt        string `json:"created_at"`
			} `json:"results"`
		}

		if err := sonic.Unmarshal(body, &apiResponse); err != nil {
			return nil, fmt.Errorf("%w: failed to parse JSON response: %w", ErrAPIFetchFailed, err)
		}

		// Build proxy URLs with authentication
		for _, proxy := range apiResponse.Results {
			proxyURL := fmt.Sprintf("http://%s:%s@%s",
				proxy.Username,
				proxy.Password,
				net.JoinHostPort(proxy.ProxyAddress, strconv.Itoa(proxy.Port)))

			parsedURL, err := url.Parse(proxyURL)
			if err != nil {
				return nil, fmt.Errorf("failed to parse proxy URL: %w", err)
			}

			allProxies = append(allProxies, parsedURL)
		}

		// Check if there are more pages
		if apiResponse.Next == "" {
			break
		}

		page++
	}

	if len(allProxies) == 0 {
		return nil, fmt.Errorf("%w: no proxies returned from API", ErrAPIFetchFailed)
	}

	zapLogger.Info("Successfully fetched roverse proxies from API", zap.Int("count", len(allProxies)))

	return allProxies, nil
}

// fetchRegularProxiesFromAPI fetches regular proxy list from configured API endpoint.
// Returns proxies in IP:Port format without authentication credentials.
func fetchRegularProxiesFromAPI(ctx context.Context, baseURL, apiKey string, zapLogger *zap.Logger) ([]*url.URL, error) {
	// Build URL with query parameters
	params := url.Values{}
	params.Add("auth", apiKey)
	params.Add("type", "displayproxies")
	params.Add("country[]", "all")
	params.Add("protocol", "http")
	params.Add("format", "normal")
	params.Add("status", "all")

	fullURL := baseURL + "?" + params.Encode()

	zapLogger.Debug("Fetching proxies from API", zap.String("url", baseURL))

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create request: %w", ErrAPIFetchFailed, err)
	}

	// Make HTTP GET request
	resp, err := proxyAPIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAPIFetchFailed, err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrAPIFetchFailed, resp.StatusCode, string(body))
	}

	// Read and parse response body
	var proxies []*url.URL

	scanner := bufio.NewScanner(resp.Body)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		// Parse IP:Port format
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("%w at line %d: %s (expected IP:Port)", ErrInvalidProxyResponse, lineNum, line)
		}

		ip, port := parts[0], parts[1]

		// Build proxy URL without authentication
		proxyURL := "http://" + net.JoinHostPort(ip, port)

		// Parse the proxy URL
		parsedURL, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse proxy URL at line %d: %w", lineNum, err)
		}

		proxies = append(proxies, parsedURL)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: error reading response: %w", ErrAPIFetchFailed, err)
	}

	if len(proxies) == 0 {
		return nil, fmt.Errorf("%w: no proxies returned from API", ErrAPIFetchFailed)
	}

	zapLogger.Info("Successfully fetched proxies from API", zap.Int("count", len(proxies)))

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
