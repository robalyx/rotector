package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jaxron/axonet/pkg/client/logger"
	"github.com/jaxron/axonet/pkg/client/middleware"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/common/setup/client/middleware/proxy/scripts"
	"github.com/robalyx/rotector/internal/common/setup/config"
)

const (
	// RotationKeyPrefix is the prefix for proxy rotation keys in Redis.
	RotationKeyPrefix = "proxy_rotation"
	// EndpointKeyPrefix is the prefix for endpoint tracking keys in Redis.
	EndpointKeyPrefix = "proxy_endpoints"
	// LastSuccessKeyPrefix is the prefix for storing last successful proxy index per endpoint.
	LastSuccessKeyPrefix = "proxy_last_success"
	// UnhealthyKeyPrefix is the prefix for storing unhealthy proxy status.
	UnhealthyKeyPrefix = "proxy_unhealthy"
)

var (
	// ErrInvalidTransport is returned when the HTTP client's transport is not compatible.
	ErrInvalidTransport = errors.New("invalid transport")
	// ErrNoHealthyProxies is returned when all proxies are marked as unhealthy.
	ErrNoHealthyProxies = errors.New("no healthy proxies available")
)

// NumericIDPattern matches any sequence of digits for path normalization.
var NumericIDPattern = regexp.MustCompile(`^\d+$`)

// EndpointPattern is a regex pattern for an endpoint.
type EndpointPattern struct {
	regex    *regexp.Regexp
	cooldown time.Duration
}

// Proxies manages proxy rotation and endpoint-specific rate limiting for HTTP requests.
type Proxies struct {
	proxies           []*url.URL
	client            rueidis.Client
	proxyClients      map[string]*http.Client
	cleanupMutex      sync.Mutex
	logger            logger.Logger
	defaultCooldown   time.Duration
	unhealthyDuration time.Duration
	endpoints         []EndpointPattern
	proxyHash         string
	rotationKey       string
	numProxies        string
}

// New creates a new Proxies instance.
func New(proxies []*url.URL, client rueidis.Client, cfg *config.Proxy) *Proxies {
	patterns := make([]EndpointPattern, 0, len(cfg.Endpoints))
	proxyHash := generateProxyHash(proxies)

	// Create HTTP clients for all proxies
	proxyClients := make(map[string]*http.Client, len(proxies))
	for _, proxy := range proxies {
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxy),
			DialContext: (&net.Dialer{
				Timeout:   20 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		}

		proxyClients[proxy.String()] = &http.Client{
			Transport: transport,
			Timeout:   time.Duration(cfg.RequestTimeout) * time.Millisecond,
		}
	}

	for _, endpoint := range cfg.Endpoints {
		// Build the regex pattern
		parts := strings.Split(endpoint.Pattern, "/")
		for i, part := range parts {
			// Escape dots only in the hostname (first part)
			if i == 0 {
				parts[i] = strings.ReplaceAll(part, ".", `\.`)
				continue
			}
			// Replace placeholders with regex pattern
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				parts[i] = "[^/]+"
			}
		}

		pattern := "^" + strings.Join(parts, "/") + "$"
		regex := regexp.MustCompile(pattern)

		patterns = append(patterns, EndpointPattern{
			regex:    regex,
			cooldown: time.Duration(endpoint.Cooldown) * time.Millisecond,
		})
	}

	return &Proxies{
		proxies:           proxies,
		client:            client,
		proxyClients:      proxyClients,
		logger:            &logger.NoOpLogger{},
		defaultCooldown:   time.Duration(cfg.DefaultCooldown) * time.Millisecond,
		unhealthyDuration: time.Duration(cfg.UnhealthyDuration) * time.Millisecond,
		endpoints:         patterns,
		proxyHash:         proxyHash,
		rotationKey:       fmt.Sprintf("%s:%s", RotationKeyPrefix, proxyHash),
		numProxies:        strconv.Itoa(len(proxies)),
	}
}

// Cleanup closes idle connections in the transport pool.
func (m *Proxies) Cleanup() {
	m.cleanupMutex.Lock()
	defer m.cleanupMutex.Unlock()

	for _, client := range m.proxyClients {
		client.CloseIdleConnections()
	}
	m.proxyClients = make(map[string]*http.Client)
}

// Process applies proxy logic before passing the request to the next middleware.
func (m *Proxies) Process(ctx context.Context, httpClient *http.Client, req *http.Request, next middleware.NextFunc) (*http.Response, error) {
	if len(m.proxies) == 0 {
		return next(ctx, httpClient, req)
	}

	// Get normalized endpoint path
	endpoint := fmt.Sprintf("%s%s", req.Host, getNormalizedPath(req.URL.Path))

	// Select proxy for endpoint
	proxy, proxyIndex, err := m.selectProxyForEndpoint(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to select proxy: %w", err)
	}

	// Apply proxy to client
	httpClient = m.applyProxyToClient(httpClient, proxy)

	// Make the request
	resp, err := next(ctx, httpClient, req)
	if err != nil {
		return nil, m.checkResponseError(ctx, err, proxy)
	}

	m.logger.WithFields(
		logger.String("endpoint", endpoint),
		logger.Int64("index", proxyIndex),
	).Debug("Used proxy")

	return resp, nil
}

// getCooldown returns the cooldown duration for a given endpoint
// It matches the endpoint against configured patterns and returns the default if no match is found.
func (m *Proxies) getCooldown(endpoint string) time.Duration {
	for _, pattern := range m.endpoints {
		if pattern.regex.MatchString(endpoint) {
			return pattern.cooldown
		}
	}
	return m.defaultCooldown
}

// selectProxyForEndpoint chooses an appropriate proxy for the given endpoint
func (m *Proxies) selectProxyForEndpoint(ctx context.Context, endpoint string) (*url.URL, int64, error) {
	now := time.Now().Unix()
	lastSuccessKey := fmt.Sprintf("%s:%s:%s", LastSuccessKeyPrefix, m.proxyHash, endpoint)

	// Get the cooldown for this endpoint
	cooldown := m.getCooldown(endpoint)

	// Execute the Lua script to get a suitable proxy
	resp := m.client.Do(ctx, m.client.B().Eval().
		Script(scripts.ProxySelection).
		Numkeys(2).
		Key(m.rotationKey).
		Key(lastSuccessKey).
		Arg(m.numProxies).
		Arg(endpoint).
		Arg(strconv.FormatInt(now, 10)).
		Arg(strconv.FormatInt(int64(cooldown.Seconds()), 10)).
		Arg(m.proxyHash).
		Build())

	if resp.Error() != nil {
		return nil, -1, fmt.Errorf("redis error: %w", resp.Error())
	}

	// Parse the response array
	result, err := resp.ToArray()
	if err != nil {
		return nil, -1, fmt.Errorf("failed to parse redis response: %w", err)
	}

	// Parse index from response
	index, err := result[0].AsInt64()
	if err != nil {
		return nil, -1, fmt.Errorf("failed to parse proxy index: %w", err)
	}

	// Special case: no healthy proxies available
	if index == -1 {
		return nil, -1, ErrNoHealthyProxies
	}

	// Parse ready timestamp from response
	readyTimestamp, err := result[1].AsInt64()
	if err != nil {
		return nil, -1, fmt.Errorf("failed to parse ready timestamp: %w", err)
	}

	// Check if we need to wait for the proxy to be ready
	if readyTimestamp > now {
		waitTime := time.Duration(readyTimestamp-now) * time.Second
		m.logger.WithFields(
			logger.String("endpoint", endpoint),
			logger.Int64("proxy_index", index),
			logger.String("wait_time", waitTime.String()),
		).Warn("Proxy on cooldown")

		select {
		case <-ctx.Done():
			return nil, -1, ctx.Err()
		case <-time.After(waitTime):
			// Use the same proxy after waiting
			return m.proxies[index], index, nil
		}
	}

	return m.proxies[index], index, nil
}

// applyProxyToClient applies the proxy to the given http.Client.
func (m *Proxies) applyProxyToClient(httpClient *http.Client, proxy *url.URL) *http.Client {
	proxyStr := proxy.String()
	proxyClient := m.proxyClients[proxyStr]

	// Copy settings from original client
	proxyClient.CheckRedirect = httpClient.CheckRedirect
	proxyClient.Jar = httpClient.Jar

	return proxyClient
}

// checkResponseError checks if the error is a timeout/network error and marks the proxy as unhealthy if it is.
func (m *Proxies) checkResponseError(ctx context.Context, err error, proxy *url.URL) error {
	if isTimeoutError(err) {
		// Find the index of the proxy
		proxyIndex := -1
		for i, p := range m.proxies {
			if p.String() == proxy.String() {
				proxyIndex = i
			}
		}

		// Mark the proxy as unhealthy
		if proxyIndex >= 0 {
			unhealthyKey := fmt.Sprintf("%s:%s:%d", UnhealthyKeyPrefix, m.proxyHash, proxyIndex)
			err := m.client.Do(ctx, m.client.B().Set().Key(unhealthyKey).Value("1").
				Px(m.unhealthyDuration).Build()).Error()
			// If we fail to mark the proxy as unhealthy, continue with the original error
			if err != nil {
				return fmt.Errorf("failed to mark proxy as unhealthy: %w", err)
			}
		}
	}

	return err
}

// SetLogger sets the logger for the middleware.
func (m *Proxies) SetLogger(l logger.Logger) {
	m.logger = l
}

// generateProxyHash creates a consistent hash for a list of proxies.
// The hash is used to namespace Redis keys and ensure different proxy lists don't interfere.
func generateProxyHash(proxies []*url.URL) string {
	if len(proxies) == 0 {
		return "empty"
	}

	// Convert proxies to strings and sort them for consistency
	proxyStrings := make([]string, len(proxies))
	for i, proxy := range proxies {
		proxyStrings[i] = proxy.String()
	}
	sort.Strings(proxyStrings)

	// Create a hash of the sorted proxy strings
	hasher := sha256.New()
	hasher.Write([]byte(strings.Join(proxyStrings, ",")))
	return hex.EncodeToString(hasher.Sum(nil))
}

// getNormalizedPath returns a normalized path where numeric IDs are replaced with placeholders.
// This ensures that paths with different IDs are treated as the same endpoint.
func getNormalizedPath(path string) string {
	// Split path into parts
	parts := strings.Split(path, "/")

	// Replace numeric IDs with placeholders
	for i, part := range parts {
		if NumericIDPattern.MatchString(part) {
			parts[i] = "{id}"
		}
	}

	return strings.Join(parts, "/")
}

// isTimeoutError checks if an error is related to timeouts, connection issues,
// or other network-related problems that would indicate a proxy is not working.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host")
}
