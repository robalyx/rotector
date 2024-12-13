package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jaxron/axonet/pkg/client/logger"
	"github.com/jaxron/axonet/pkg/client/middleware"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/setup/client/middleware/proxy/scripts"
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
	logger            logger.Logger
	requestTimeout    time.Duration
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
		logger:            &logger.NoOpLogger{},
		requestTimeout:    time.Duration(cfg.RequestTimeout) * time.Millisecond,
		defaultCooldown:   time.Duration(cfg.DefaultCooldown) * time.Millisecond,
		unhealthyDuration: time.Duration(cfg.UnhealthyDuration) * time.Millisecond,
		endpoints:         patterns,
		proxyHash:         proxyHash,
		rotationKey:       fmt.Sprintf("%s:%s", RotationKeyPrefix, proxyHash),
		numProxies:        strconv.Itoa(len(proxies)),
	}
}

// Process applies proxy logic before passing the request to the next middleware.
// It selects an available proxy based on endpoint cooldowns and configures the HTTP client.
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
	httpClient, err = m.applyProxyToClient(httpClient, proxy)
	if err != nil {
		return nil, err
	}

	// Make the request
	resp, err := next(ctx, httpClient, req)
	if err != nil {
		return nil, m.checkResponseError(ctx, err, proxy)
	}

	// Update the timestamp after the request completes successfully
	cooldown := m.getCooldown(endpoint)
	if updateErr := m.updateProxyTimestamp(ctx, endpoint, proxyIndex, cooldown); updateErr != nil {
		m.logger.WithFields(
			logger.String("endpoint", endpoint),
			logger.String("error", updateErr.Error()),
		).Error("Failed to update proxy timestamp")
	}

	m.logger.WithFields(
		logger.String("endpoint", endpoint),
		logger.Int64("index", proxyIndex),
		logger.String("cooldown", cooldown.String()),
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
// It uses Redis to track endpoint usage and ensure proper cooldown periods.
func (m *Proxies) selectProxyForEndpoint(ctx context.Context, endpoint string) (*url.URL, int64, error) {
	now := time.Now().Unix()
	lastSuccessKey := fmt.Sprintf("%s:%s:%s", LastSuccessKeyPrefix, m.proxyHash, endpoint)

	// Execute the Lua script to get a suitable proxy
	resp := m.client.Do(ctx, m.client.B().Eval().
		Script(scripts.ProxySelection).
		Numkeys(2).
		Key(m.rotationKey).
		Key(lastSuccessKey).
		Arg(m.numProxies).
		Arg(endpoint).
		Arg(strconv.FormatInt(now, 10)).
		Arg(strconv.FormatInt(int64(m.requestTimeout.Seconds()), 10)).
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

	// Parse wait time from response
	waitSeconds, err := result[1].AsInt64()
	if err != nil {
		return nil, -1, fmt.Errorf("failed to parse wait time: %w", err)
	}

	// Check if we need to wait for the proxy to be ready
	if waitSeconds > 0 {
		waitTime := time.Duration(waitSeconds) * time.Second
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

// applyProxyToClient applies the proxy to the given http.Client
// It creates a new client with a cloned transport to avoid modifying the original.
func (m *Proxies) applyProxyToClient(httpClient *http.Client, proxy *url.URL) (*http.Client, error) {
	// Get the transport from the client
	transport, err := m.getTransport(httpClient)
	if err != nil {
		return nil, err
	}

	// Clone the transport
	newTransport := transport.Clone()

	// Modify only the necessary fields
	newTransport.Proxy = http.ProxyURL(proxy)
	newTransport.OnProxyConnectResponse = func(_ context.Context, proxyURL *url.URL, req *http.Request, _ *http.Response) error {
		m.logger.WithFields(
			logger.String("proxy", proxyURL.Host),
			logger.String("url", req.URL.String()),
		).Debug("Proxy connection established")
		return nil
	}

	// Create a new client with the modified transport
	return &http.Client{
		Transport:     newTransport,
		CheckRedirect: httpClient.CheckRedirect,
		Jar:           httpClient.Jar,
		Timeout:       httpClient.Timeout,
	}, nil
}

// updateProxyTimestamp updates the timestamp for when the proxy was actually used.
func (m *Proxies) updateProxyTimestamp(ctx context.Context, endpoint string, proxyIndex int64, cooldown time.Duration) error {
	endpointKey := fmt.Sprintf("proxy_endpoints:%s:%d", m.proxyHash, proxyIndex)
	nextAvailable := time.Now().Add(cooldown).Unix()

	// Update the timestamp to when the endpoint will be available again
	err := m.client.Do(ctx, m.client.B().Zadd().
		Key(endpointKey).
		ScoreMember().
		ScoreMember(float64(nextAvailable), endpoint).
		Build()).Error()

	return err
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

// getTransport extracts the http.Transport from the client.
// Returns the default transport if none is set, or an error if the transport is incompatible.
func (m *Proxies) getTransport(httpClient *http.Client) (*http.Transport, error) {
	if t, ok := httpClient.Transport.(*http.Transport); ok {
		return t, nil
	}
	if httpClient.Transport == nil {
		return http.DefaultTransport.(*http.Transport), nil
	}
	return nil, ErrInvalidTransport
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
