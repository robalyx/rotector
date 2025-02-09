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

	axonetErrors "github.com/jaxron/axonet/pkg/client/errors"
	"github.com/jaxron/axonet/pkg/client/logger"
	"github.com/jaxron/axonet/pkg/client/middleware"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/common/setup/client/middleware/proxy/scripts"
	"github.com/robalyx/rotector/internal/common/setup/config"
	"golang.org/x/sync/semaphore"
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

	// RoverseAuthHeaderName is the name of the authentication header for roverse.
	RoverseAuthHeaderName = "X-Proxy-Secret"
)

var (
	// ErrNoHealthyProxies is returned when all proxies are marked as unhealthy.
	ErrNoHealthyProxies = errors.New("no healthy proxies available")
	// ErrProxyOnCooldown is returned when a proxy is on cooldown.
	ErrProxyOnCooldown = errors.New("proxy is on cooldown")
	// ErrTooManyRequests is returned when the request is rate limited.
	ErrTooManyRequests = errors.New("too many requests")

	// ErrMissingRoverseDomain is returned when the Roverse domain is not configured.
	ErrMissingRoverseDomain = errors.New("roverse domain is not configured")
	// ErrMissingRoverseSecretKey is returned when the secret key is not configured.
	ErrMissingRoverseSecretKey = errors.New("roverse secret key is not configured")
	// ErrNotRobloxDomain is returned when the request is not for a Roblox domain.
	ErrNotRobloxDomain = errors.New("request is not for a Roblox domain")
)

var (
	// NumericIDPattern matches any sequence of digits for path normalization.
	NumericIDPattern = regexp.MustCompile(`^\d+$`)
	// CDNHashPattern matches the hash portion of CDN URLs.
	CDNHashPattern = regexp.MustCompile(`30DAY-Avatar-[0-9A-F]{32}-Png`)
)

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
	roverseDomain     string
	roverseSecretKey  string
	roverseSem        *semaphore.Weighted
}

// New creates a new Proxies instance.
func New(proxies []*url.URL, client rueidis.Client, cfg *config.CommonConfig, requestTimeout time.Duration) *Proxies {
	patterns := make([]EndpointPattern, 0, len(cfg.Proxy.Endpoints))
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
			Timeout:   requestTimeout,
		}
	}

	for _, endpoint := range cfg.Proxy.Endpoints {
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

	// Setup roverse if configured
	var roverseDomain string
	var roverseSecretKey string
	var roverseSem *semaphore.Weighted

	if cfg.Roverse.Domain != "" {
		if cfg.Roverse.SecretKey == "" {
			panic(ErrMissingRoverseSecretKey)
		}

		// Clean the domain by removing any protocol prefix and trailing slashes
		roverseDomain = strings.TrimRight(cfg.Roverse.Domain, "/")
		roverseDomain = strings.TrimPrefix(roverseDomain, "https://")
		roverseDomain = strings.TrimPrefix(roverseDomain, "http://")

		roverseSecretKey = cfg.Roverse.SecretKey
		roverseSem = semaphore.NewWeighted(cfg.Roverse.MaxConcurrent)
	}

	return &Proxies{
		proxies:           proxies,
		client:            client,
		proxyClients:      proxyClients,
		logger:            &logger.NoOpLogger{},
		defaultCooldown:   time.Duration(cfg.Proxy.DefaultCooldown) * time.Millisecond,
		unhealthyDuration: time.Duration(cfg.Proxy.UnhealthyDuration) * time.Millisecond,
		endpoints:         patterns,
		proxyHash:         proxyHash,
		rotationKey:       fmt.Sprintf("%s:%s", RotationKeyPrefix, proxyHash),
		numProxies:        strconv.Itoa(len(proxies)),
		roverseDomain:     roverseDomain,
		roverseSecretKey:  roverseSecretKey,
		roverseSem:        roverseSem,
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
	// Try proxy first
	var resp *http.Response
	var err error
	if len(m.proxies) > 0 {
		resp, err = m.tryProxy(ctx, httpClient, req, next)
		if err == nil {
			return resp, nil
		}
		m.logger.WithFields(
			logger.String("error", err.Error()),
		).Debug("Proxy attempt failed")
	}

	// Try roverse as fallback
	var roverseErr error
	resp, roverseErr = m.tryRoverse(ctx, httpClient, req)
	if err != nil {
		if errors.Is(err, ErrMissingRoverseDomain) || errors.Is(err, ErrNotRobloxDomain) {
			return nil, fmt.Errorf("%w: %w", axonetErrors.ErrTemporary, err)
		}
		return nil, fmt.Errorf("%w: %w", axonetErrors.ErrTemporary, roverseErr)
	}

	return resp, nil
}

// tryProxy attempts to use a proxy for the given endpoint.
func (m *Proxies) tryProxy(ctx context.Context, httpClient *http.Client, req *http.Request, next middleware.NextFunc) (*http.Response, error) {
	// Get normalized endpoint path
	endpoint := fmt.Sprintf("%s%s", req.Host, getNormalizedPath(req.URL.Path))

	// Select proxy for endpoint
	proxy, proxyIndex, err := m.selectProxyForEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	// Apply proxy to client
	proxyClient := m.applyProxyToClient(httpClient, proxy)

	// Make the request
	resp, err := next(ctx, proxyClient, req)
	if err != nil {
		// Try selecting another proxy on timeout errors
		if isTimeoutError(err) {
			unhealthyKey := fmt.Sprintf("%s:%s:%d", UnhealthyKeyPrefix, m.proxyHash, proxyIndex)
			if markErr := m.client.Do(ctx, m.client.B().Set().Key(unhealthyKey).Value("1").
				Px(m.unhealthyDuration).Build()).Error(); markErr != nil {
				m.logger.WithFields(
					logger.String("endpoint", endpoint),
					logger.String("error", markErr.Error()),
				).Error("Failed to mark proxy as unhealthy")
			}
			return m.tryProxy(ctx, httpClient, req, next)
		}
		return nil, err
	}

	// Check for rate limit response
	if resp.StatusCode == http.StatusTooManyRequests {
		return resp, ErrTooManyRequests
	}

	return resp, nil
}

// tryRoverse attempts to use the roverse proxy for the given request.
func (m *Proxies) tryRoverse(ctx context.Context, httpClient *http.Client, req *http.Request) (*http.Response, error) {
	// Skip if roverse is not configured
	if m.roverseDomain == "" {
		return nil, ErrMissingRoverseDomain
	}

	// Skip non-Roblox domains
	if !strings.HasSuffix(req.Host, ".roblox.com") {
		return nil, ErrNotRobloxDomain
	}

	// Try to acquire a slot
	if err := m.roverseSem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer m.roverseSem.Release(1)

	// Extract subdomain from host
	subdomain := strings.TrimSuffix(req.Host, ".roblox.com")

	// Create new URL for the roverse proxy
	proxyURL := fmt.Sprintf("https://%s.%s%s", subdomain, m.roverseDomain, req.URL.Path)
	if req.URL.RawQuery != "" {
		proxyURL += "?" + req.URL.RawQuery
	}

	// Create new request
	proxyReq, err := http.NewRequestWithContext(ctx, req.Method, proxyURL, req.Body)
	if err != nil {
		return nil, err
	}

	// Copy all headers from original request
	proxyReq.Header = make(http.Header, len(req.Header))
	for key, values := range req.Header {
		proxyReq.Header[key] = values
	}

	// Add authentication header
	proxyReq.Header.Set(RoverseAuthHeaderName, m.roverseSecretKey)

	m.logger.WithFields(
		logger.String("original_url", req.URL.String()),
		logger.String("proxy_url", proxyURL),
	).Debug("Routing request through roverse (fallback)")

	// Make the request
	resp, err := httpClient.Do(proxyReq)
	if err != nil {
		m.logger.WithFields(
			logger.String("error", err.Error()),
		).Debug("Roverse attempt failed")
		return nil, err
	}

	// Check for rate limit from roverse
	if resp.StatusCode == http.StatusTooManyRequests {
		return resp, ErrTooManyRequests
	}

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

	// Parse cooldown status from response
	cooldownStatus, err := result[1].AsInt64()
	if err != nil {
		return nil, -1, fmt.Errorf("failed to parse cooldown status: %w", err)
	}

	if cooldownStatus == 1 {
		return nil, index, ErrProxyOnCooldown
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

// getNormalizedPath returns a normalized path where numeric IDs and CDN hashes are replaced with placeholders.
func getNormalizedPath(path string) string {
	// Normalize any CDN hash patterns in the full path
	path = CDNHashPattern.ReplaceAllString(path, "30DAY-Avatar-{hash}-Png")

	// Split path into parts and handle numeric IDs
	parts := strings.Split(path, "/")
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
