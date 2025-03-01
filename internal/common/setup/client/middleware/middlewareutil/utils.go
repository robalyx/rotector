package middlewareutil

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// GenerateProxyHash creates a consistent hash for a list of proxies.
// The hash is used to namespace Redis keys and ensure different proxy lists don't interfere.
func GenerateProxyHash(proxies []*url.URL) string {
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

// IsTimeoutError checks if an error is related to timeouts, connection issues,
// or other network-related problems that would indicate a proxy is not working.
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host")
}
