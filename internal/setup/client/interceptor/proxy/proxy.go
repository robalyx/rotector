package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jaxron/axonet/pkg/client/logger"
	"github.com/jaxron/axonet/pkg/client/middleware"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/setup/client/interceptor/interceptorutil"
	"github.com/robalyx/rotector/internal/setup/client/interceptor/proxy/scripts"
	"github.com/robalyx/rotector/internal/setup/config"
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

// Middleware manages proxy rotation and endpoint-specific rate limiting for HTTP requests.
type Middleware struct {
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

// New creates a new Middleware instance.
func New(proxies []*url.URL, client rueidis.Client, cfg *config.CommonConfig, requestTimeout time.Duration) *Middleware {
	patterns := make([]EndpointPattern, 0, len(cfg.Proxy.Endpoints))
	proxyHash := interceptorutil.GenerateProxyHash(proxies)

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

	return &Middleware{
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
	}
}

// Cleanup closes idle connections in the transport pool.
func (m *Middleware) Cleanup() {
	m.cleanupMutex.Lock()
	defer m.cleanupMutex.Unlock()

	for _, client := range m.proxyClients {
		client.CloseIdleConnections()
	}
	m.proxyClients = make(map[string]*http.Client)
}

// Process applies proxy logic before passing the request to the next middleware.
func (m *Middleware) Process(
	ctx context.Context, httpClient *http.Client, req *http.Request, next middleware.NextFunc,
) (*http.Response, error) {
	// Skip if no proxies are available
	if len(m.proxies) == 0 {
		return next(ctx, httpClient, req)
	}

	// Try to use a proxy
	resp, err := m.tryProxy(ctx, httpClient, req)
	if err != nil {
		m.logger.WithFields(
			logger.String("error", err.Error()),
			logger.String("url", req.URL.String()),
		).Debug("Proxy attempt failed")

		// Continue to next middleware (which could be Roverse)
		return next(ctx, httpClient, req)
	}

	return resp, nil
}

// SetLogger sets the logger for the middleware.
func (m *Middleware) SetLogger(l logger.Logger) {
	m.logger = l
}

// GetProxies returns the list of proxies.
func (m *Middleware) GetProxies() []*url.URL {
	return m.proxies
}

// tryProxy attempts to use a proxy for the given endpoint.
func (m *Middleware) tryProxy(ctx context.Context, httpClient *http.Client, req *http.Request) (*http.Response, error) {
	// Get normalized endpoint path
	endpoint := fmt.Sprintf("%s%s", req.Host, GetNormalizedPath(req.URL.Path))

	// Select proxy for endpoint
	proxy, proxyIndex, err := m.selectProxyForEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	// Apply proxy to client
	proxyClient := m.applyProxyToClient(httpClient, proxy)

	// Make the request
	resp, err := proxyClient.Do(req)
	if err != nil {
		// Check if error is due to context cancellation/timeout
		if ctx.Err() != nil {
			return nil, err
		}
		// Try selecting another proxy on timeout errors
		if interceptorutil.IsTimeoutError(err) {
			m.markProxyUnhealthy(ctx, proxyIndex)
			return m.tryProxy(ctx, httpClient, req)
		}
		return nil, err
	}

	// Check if the response indicates success
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		m.logger.WithFields(
			logger.String("url", req.URL.String()),
			logger.Int("status_code", resp.StatusCode),
		).Debug("Proxy attempt succeeded")

		return resp, nil
	}

	m.logger.WithFields(
		logger.String("url", req.URL.String()),
		logger.Int("status_code", resp.StatusCode),
	).Debug("Proxy attempt failed")

	return resp, nil
}

// getCooldown returns the cooldown duration for a given endpoint
// It matches the endpoint against configured patterns and returns the default if no match is found.
func (m *Middleware) getCooldown(endpoint string) time.Duration {
	for _, pattern := range m.endpoints {
		if pattern.regex.MatchString(endpoint) {
			return pattern.cooldown
		}
	}
	return m.defaultCooldown
}

// selectProxyForEndpoint chooses an appropriate proxy for the given endpoint.
func (m *Middleware) selectProxyForEndpoint(ctx context.Context, endpoint string) (*url.URL, int64, error) {
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
		return nil, -1, interceptorutil.ErrAllProxiesUnhealthy
	}

	// Parse cooldown status from response
	cooldownStatus, err := result[1].AsInt64()
	if err != nil {
		return nil, -1, fmt.Errorf("failed to parse cooldown status: %w", err)
	}

	if cooldownStatus == 1 {
		return nil, index, interceptorutil.ErrProxyOnCooldown
	}

	return m.proxies[index], index, nil
}

// markProxyUnhealthy marks a proxy as unhealthy for a period of time.
func (m *Middleware) markProxyUnhealthy(ctx context.Context, proxyIndex int64) {
	if len(m.proxies) == 0 || proxyIndex < 0 {
		return
	}

	unhealthyKey := fmt.Sprintf("%s:%s:%d", UnhealthyKeyPrefix, m.proxyHash, proxyIndex)
	err := m.client.Do(ctx, m.client.B().Set().Key(unhealthyKey).Value("1").
		Px(m.unhealthyDuration).Build()).Error()
	if err != nil {
		m.logger.WithFields(
			logger.String("error", err.Error()),
		).Error("Failed to mark proxy as unhealthy")
		return
	}

	m.logger.WithFields(
		logger.Int64("proxy_index", proxyIndex),
		logger.Duration("unhealthy_duration", m.unhealthyDuration),
	).Debug("Marked proxy as unhealthy")
}

// applyProxyToClient applies the proxy to the given http.Client.
func (m *Middleware) applyProxyToClient(httpClient *http.Client, proxy *url.URL) *http.Client {
	proxyStr := proxy.String()
	proxyClient := m.proxyClients[proxyStr]

	// Copy settings from original client
	proxyClient.CheckRedirect = httpClient.CheckRedirect
	proxyClient.Jar = httpClient.Jar

	return proxyClient
}

// GetNormalizedPath returns a normalized path where numeric IDs and CDN hashes are replaced with placeholders.
func GetNormalizedPath(path string) string {
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
