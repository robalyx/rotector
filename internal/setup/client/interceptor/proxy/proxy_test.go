package proxy_test

import (
	"testing"

	"github.com/robalyx/rotector/internal/setup/client/interceptor/proxy"
	"github.com/stretchr/testify/assert"
)

func TestGetNormalizedPath(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			result := proxy.GetNormalizedPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
