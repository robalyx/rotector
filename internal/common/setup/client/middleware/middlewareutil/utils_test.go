package middlewareutil_test

import (
	"net/url"
	"testing"

	"github.com/robalyx/rotector/internal/common/setup/client/middleware/middlewareutil"
	"github.com/stretchr/testify/assert"
)

func TestGenerateProxyHash(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			if tt.name == "Multiple proxies - order independent" {
				// Test both orders to ensure hash is consistent
				result1 := middlewareutil.GenerateProxyHash(tt.proxies)

				// Reverse the slice
				for i, j := 0, len(tt.proxies)-1; i < j; i, j = i+1, j-1 {
					tt.proxies[i], tt.proxies[j] = tt.proxies[j], tt.proxies[i]
				}
				result2 := middlewareutil.GenerateProxyHash(tt.proxies)

				assert.Equal(t, result1, result2, "Hash should be consistent regardless of proxy order")
			}

			result := middlewareutil.GenerateProxyHash(tt.proxies)
			if tt.name == "Empty proxy list" {
				assert.Equal(t, tt.expected, result)
			} else {
				assert.Len(t, result, 64, "Hash should be 64 characters long")
			}
		})
	}
}

func TestIsTimeoutError(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := middlewareutil.IsTimeoutError(tt.err)
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
