package setup

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jaxron/axonet/middleware/proxy"
	"github.com/jaxron/axonet/middleware/rediscache"
	"github.com/jaxron/axonet/middleware/retry"
	"github.com/jaxron/axonet/middleware/singleflight"
	"github.com/jaxron/axonet/pkg/client"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/config"
	"go.uber.org/zap"
)

// GetRoAPIClient creates a new RoAPI client with the given configuration.
func GetRoAPIClient(cfg config.Roblox, redis config.Redis, logger *zap.Logger) *api.API {
	// Read the cookies and proxies
	cookies := readCookies(cfg, logger)
	proxies := readProxies(cfg, logger)

	// Initialize Redis cache
	cache, err := rediscache.New(rueidis.ClientOption{
		InitAddress: []string{fmt.Sprintf("%s:%d", redis.Host, redis.Port)},
		Username:    redis.Username,
		Password:    redis.Password,
	}, 5*time.Minute)
	if err != nil {
		logger.Fatal("failed to create Redis cache", zap.Error(err))
		return nil
	}

	return api.New(cookies,
		client.WithLogger(NewLogger(logger)),
		client.WithTimeout(10*time.Second),
		client.WithMiddleware(cache),
		client.WithMiddleware(retry.New(5, 500*time.Millisecond, 1000*time.Millisecond)),
		client.WithMiddleware(singleflight.New()),
		client.WithMiddleware(proxy.New(proxies)),
	)
}

// readProxies reads the proxies from the given configuration file.
func readProxies(cfg config.Roblox, logger *zap.Logger) []*url.URL {
	// If no proxies file is set, return an empty list
	if cfg.ProxiesFile == "" {
		logger.Warn("No proxies file set")
		return []*url.URL{}
	}

	var proxies []*url.URL

	// Open the file
	file, err := os.Open(cfg.ProxiesFile)
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
func readCookies(cfg config.Roblox, logger *zap.Logger) []string {
	// If no cookies file is set, return an empty list
	if cfg.CookiesFile == "" {
		logger.Warn("No cookies file set")
		return []string{}
	}

	var cookies []string

	// Open the file
	file, err := os.Open(cfg.CookiesFile)
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
