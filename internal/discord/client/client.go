package client

import (
	"hash/fnv"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/state/store/defaultstore"
	"github.com/diamondburned/arikawa/v3/utils/handler"
	"github.com/diamondburned/arikawa/v3/utils/httputil"
	"github.com/diamondburned/arikawa/v3/utils/httputil/httpdriver"
)

// SelectProxyForToken deterministically selects a proxy for a given Discord token.
// Returns the selected proxy URL and its index in the proxy list.
func SelectProxyForToken(token string, proxies []*url.URL) (*url.URL, int) {
	if len(proxies) == 0 {
		return nil, -1
	}

	// Use FNV-1a hash for deterministic selection
	h := fnv.New64a()
	h.Write([]byte(token))
	hash := h.Sum64()

	// Use modulo to select proxy index
	//nolint:gosec // G115: conversion is safe as modulo ensures value is within array bounds
	index := int(hash % uint64(len(proxies)))

	return proxies[index], index
}

// NewHTTPClientWithProxy creates an HTTP client configured to use the specified proxy.
func NewHTTPClientWithProxy(proxy *url.URL, timeout time.Duration) *http.Client {
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

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

// NewStateWithProxy creates an arikawa State configured to use the specified proxy.
func NewStateWithProxy(token string, proxy *url.URL, intents gateway.Intents) *state.State {
	httpClient := NewHTTPClientWithProxy(proxy, 30*time.Second)
	driver := httpdriver.WrapClient(*httpClient)

	httputilClient := &httputil.Client{
		Client:  driver,
		Timeout: 30 * time.Second,
	}

	apiClient := api.NewCustomClient(token, httputilClient)
	identifier := gateway.DefaultIdentifier(token)
	h := handler.New()

	sess := session.NewCustom(identifier, apiClient, h)
	cabinet := defaultstore.New()

	s := state.NewFromSession(sess, cabinet)
	s.AddIntents(intents)

	s.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"
	s.Timeout = 30 * time.Second

	return s
}

// NewSessionWithProxy creates a Discord session configured to use the specified proxy.
func NewSessionWithProxy(token string, proxy *url.URL) *session.Session {
	httpClient := NewHTTPClientWithProxy(proxy, 30*time.Second)
	driver := httpdriver.WrapClient(*httpClient)

	httputilClient := &httputil.Client{
		Client:  driver,
		Timeout: 30 * time.Second,
	}

	apiClient := api.NewCustomClient(token, httputilClient)

	identifier := gateway.DefaultIdentifier(token)
	h := handler.New()
	sess := session.NewCustom(identifier, apiClient, h)

	sess.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"

	return sess
}
