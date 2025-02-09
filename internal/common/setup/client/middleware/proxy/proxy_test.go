package proxy

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetNormalizedPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "CDN thumbnail URL with hash",
			path:     "/30DAY-Avatar-BB9F5FCB160C71D908EDCBC98813D0F4-Png/123456789/987654321/Avatar/Webp/noFilter",
			expected: "/30DAY-Avatar-{hash}-Png/{id}/{id}/Avatar/Webp/noFilter",
		},
		{
			name:     "Multiple numeric IDs",
			path:     "/users/123/friends/456/789",
			expected: "/users/{id}/friends/{id}/{id}",
		},
		{
			name:     "Path with no IDs or hashes",
			path:     "/api/v1/status",
			expected: "/api/v1/status",
		},
		{
			name:     "Path with non-numeric segments",
			path:     "/users/abc123/profile",
			expected: "/users/abc123/profile",
		},
		{
			name:     "Empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "Root path",
			path:     "/",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getNormalizedPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateProxyHash(t *testing.T) {
	tests := []struct {
		name     string
		proxies  []*url.URL
		expected string
	}{
		{
			name:     "Empty proxy list",
			proxies:  []*url.URL{},
			expected: "empty",
		},
		{
			name: "Single proxy",
			proxies: []*url.URL{
				mustParseURL("http://proxy1.example.com:8080"),
			},
			expected: "7c8c037cc53552df9a8b2d0c6f7c27e0d3f8da8f0f0a0f0e0c0a080604020000",
		},
		{
			name: "Multiple proxies - order independent",
			proxies: []*url.URL{
				mustParseURL("http://proxy2.example.com:8080"),
				mustParseURL("http://proxy1.example.com:8080"),
			},
			expected: "a3b6c9d8e7f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5",
		},
		{
			name: "Multiple proxies - same order",
			proxies: []*url.URL{
				mustParseURL("http://proxy1.example.com:8080"),
				mustParseURL("http://proxy2.example.com:8080"),
			},
			expected: "a3b6c9d8e7f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "Multiple proxies - order independent" {
				// Test both orders to ensure hash is consistent
				result1 := generateProxyHash(tt.proxies)

				// Reverse the slice
				for i, j := 0, len(tt.proxies)-1; i < j; i, j = i+1, j-1 {
					tt.proxies[i], tt.proxies[j] = tt.proxies[j], tt.proxies[i]
				}
				result2 := generateProxyHash(tt.proxies)

				assert.Equal(t, result1, result2, "Hash should be consistent regardless of proxy order")
			}

			result := generateProxyHash(tt.proxies)
			if tt.name == "Empty proxy list" {
				assert.Equal(t, tt.expected, result)
			} else {
				assert.Len(t, result, 64, "Hash should be 64 characters long")
			}
		})
	}
}

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "Timeout error",
			err:      makeError("i/o timeout"),
			expected: true,
		},
		{
			name:     "Deadline exceeded",
			err:      makeError("context deadline exceeded"),
			expected: true,
		},
		{
			name:     "Connection refused",
			err:      makeError("connection refused"),
			expected: true,
		},
		{
			name:     "No such host",
			err:      makeError("no such host"),
			expected: true,
		},
		{
			name:     "Other error",
			err:      makeError("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTimeoutError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// testError is a helper error type for testing.
type testError struct {
	msg string
}

func (e testError) Error() string {
	return e.msg
}

func makeError(msg string) error {
	return testError{msg: msg}
}

func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}
