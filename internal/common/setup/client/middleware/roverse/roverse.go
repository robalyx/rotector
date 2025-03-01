package roverse

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	axonetErrors "github.com/jaxron/axonet/pkg/client/errors"
	"github.com/jaxron/axonet/pkg/client/logger"
	"github.com/jaxron/axonet/pkg/client/middleware"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/common/setup/client/middleware/middlewareutil"
	"github.com/robalyx/rotector/internal/common/setup/client/middleware/roverse/scripts"
	"github.com/robalyx/rotector/internal/common/setup/config"

	"golang.org/x/sync/semaphore"
)

const (
	// RotationKeyPrefix is the prefix for roverse proxy rotation keys in Redis.
	RotationKeyPrefix = "roverse_proxy_rotation"
	// UnhealthyKeyPrefix is the prefix for storing unhealthy roverse proxy status.
	UnhealthyKeyPrefix = "roverse_proxy_unhealthy"
	// AuthHeaderName is the name of the authentication header for roverse.
	AuthHeaderName = "X-Proxy-Secret"
)

// Middleware manages Roverse proxy routing and utilizes multiple proxies from different countries.
type Middleware struct {
	proxies           []*url.URL
	client            rueidis.Client
	proxyClients      map[string]*http.Client
	cleanupMutex      sync.Mutex
	logger            logger.Logger
	unhealthyDuration time.Duration
	proxyHash         string
	rotationKey       string
	numProxies        string
	domain            string
	secretKey         string
	requestSem        *semaphore.Weighted
}

// New creates a new Roverse middleware instance.
func New(proxies []*url.URL, client rueidis.Client, cfg *config.CommonConfig, requestTimeout time.Duration) *Middleware {
	proxyHash := middlewareutil.GenerateProxyHash(proxies)

	// Create HTTP clients for all proxies
	proxyClients := make(map[string]*http.Client, len(proxies))
	for _, proxy := range proxies {
		transport := &http.Transport{
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
		transport.Proxy = http.ProxyURL(proxy)

		proxyClients[proxy.String()] = &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
		}
	}

	// Setup roverse configuration
	var domain string
	var secretKey string
	var requestSem *semaphore.Weighted

	if cfg.Roverse.Domain != "" {
		if cfg.Roverse.SecretKey == "" {
			panic(middlewareutil.ErrMissingSecretKey)
		}

		// Clean the domain by removing any protocol prefix and trailing slashes
		domain = strings.TrimRight(cfg.Roverse.Domain, "/")
		domain = strings.TrimPrefix(domain, "https://")
		domain = strings.TrimPrefix(domain, "http://")

		secretKey = cfg.Roverse.SecretKey
		requestSem = semaphore.NewWeighted(cfg.Roverse.MaxConcurrent)
	}

	return &Middleware{
		proxies:           proxies,
		client:            client,
		proxyClients:      proxyClients,
		logger:            &logger.NoOpLogger{},
		unhealthyDuration: time.Duration(cfg.Proxy.UnhealthyDuration) * time.Millisecond,
		proxyHash:         proxyHash,
		rotationKey:       fmt.Sprintf("%s:%s", RotationKeyPrefix, proxyHash),
		numProxies:        strconv.Itoa(len(proxies)),
		domain:            domain,
		secretKey:         secretKey,
		requestSem:        requestSem,
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

// Process applies Roverse proxy logic and causes a temporary error if it fails.
func (m *Middleware) Process(ctx context.Context, httpClient *http.Client, req *http.Request, _ middleware.NextFunc) (*http.Response, error) {
	// Skip if Roverse is not configured
	if m.domain == "" {
		return nil, fmt.Errorf("%w: roverse not configured", axonetErrors.ErrTemporary)
	}

	// Skip non-Roblox domains
	if !strings.HasSuffix(req.Host, ".roblox.com") {
		return nil, fmt.Errorf("%w: roverse not configured for non-Roblox domain", axonetErrors.ErrTemporary)
	}

	// Try to route through Roverse
	resp, err := m.routeRequest(ctx, httpClient, req)
	if err != nil {
		m.logger.WithFields(
			logger.String("error", err.Error()),
			logger.String("url", req.URL.String()),
		).Debug("Roverse request failed")

		return nil, fmt.Errorf("%w: request failed: %s", axonetErrors.ErrTemporary, err.Error())
	}

	return resp, nil
}

// routeRequest attempts to route the request through the Roverse proxy.
func (m *Middleware) routeRequest(ctx context.Context, httpClient *http.Client, req *http.Request) (*http.Response, error) {
	// Try to acquire a slot
	if err := m.requestSem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer m.requestSem.Release(1)

	// Extract subdomain from host
	subdomain := strings.TrimSuffix(req.Host, ".roblox.com")

	// Create new URL for the roverse proxy
	proxyURL := fmt.Sprintf("https://%s.%s%s", subdomain, m.domain, req.URL.Path)
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
	maps.Copy(proxyReq.Header, req.Header)

	// Add authentication header
	proxyReq.Header.Set(AuthHeaderName, m.secretKey)

	// If we have proxies, use them
	client := httpClient
	proxyIndex := int64(-1)
	proxyHost := ""

	if len(m.proxies) > 0 {
		roverseProxy, index, err := m.selectProxy(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to select proxy", axonetErrors.ErrTemporary)
		}
		client = m.applyProxyToClient(httpClient, roverseProxy)
		proxyIndex = index
		proxyHost = roverseProxy.Host
	}

	// Make the request
	resp, err := client.Do(proxyReq)
	if err != nil {
		// If it's a timeout error and we're using a proxy, mark it as unhealthy
		if middlewareutil.IsTimeoutError(err) && proxyIndex >= 0 {
			m.markProxyUnhealthy(ctx, proxyIndex)
		}
		return nil, err
	}

	// Check if response is successful
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		m.logger.WithFields(
			logger.String("url", req.URL.String()),
			logger.String("proxy_host", proxyHost),
			logger.Int("status_code", resp.StatusCode),
		).Debug("Roverse request succeeded")

		return resp, nil
	}

	m.logger.WithFields(
		logger.String("url", req.URL.String()),
		logger.String("proxy_host", proxyHost),
		logger.Int("status_code", resp.StatusCode),
	).Debug("Roverse request failed")

	return resp, nil
}

// selectProxy selects a proxy to use for Roverse requests.
func (m *Middleware) selectProxy(ctx context.Context) (*url.URL, int64, error) {
	if len(m.proxies) == 0 {
		return nil, -1, middlewareutil.ErrNoProxiesAvailable
	}

	// Execute the Lua script to get a suitable proxy
	resp := m.client.Do(ctx, m.client.B().Eval().
		Script(scripts.ProxySelection).
		Numkeys(1).
		Key(m.rotationKey).
		Arg(m.numProxies).
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
		return nil, -1, middlewareutil.ErrAllProxiesUnhealthy
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
	} else {
		m.logger.WithFields(
			logger.Int64("proxy_index", proxyIndex),
			logger.Duration("unhealthy_duration", m.unhealthyDuration),
		).Debug("Marked proxy as unhealthy")
	}
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

// SetLogger sets the logger for the middleware.
func (m *Middleware) SetLogger(l logger.Logger) {
	m.logger = l
}

// GetProxies returns the list of Roverse proxies.
func (m *Middleware) GetProxies() []*url.URL {
	return m.proxies
}
