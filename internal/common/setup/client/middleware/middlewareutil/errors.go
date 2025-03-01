package middlewareutil

import "errors"

var (
	// ErrTooManyRequests is returned when the request is rate limited.
	ErrTooManyRequests = errors.New("too many requests")
	// ErrMissingSecretKey is returned when the secret key is not configured.
	ErrMissingSecretKey = errors.New("roverse secret key is not configured")
	// ErrNoProxiesAvailable is returned when no proxies are available.
	ErrNoProxiesAvailable = errors.New("no roverse proxies available")
	// ErrAllProxiesUnhealthy is returned when all proxies are marked as unhealthy.
	ErrAllProxiesUnhealthy = errors.New("all roverse proxies are unhealthy")
	// ErrProxyOnCooldown is returned when a proxy is on cooldown.
	ErrProxyOnCooldown = errors.New("proxy is on cooldown")
)
