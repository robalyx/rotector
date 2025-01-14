package utils

import (
	"testing"

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/stretchr/testify/assert"
)

// mockSession implements a simple in-memory session for testing.
type mockSession struct {
	data map[string]interface{}
}

func newMockSession() *mockSession {
	return &mockSession{
		data: make(map[string]interface{}),
	}
}

func (s *mockSession) Set(key string, value interface{}) {
	s.data[key] = value
}

func (s *mockSession) GetInt(key string) int {
	if value, ok := s.data[key]; ok {
		if intValue, ok := value.(int); ok {
			return intValue
		}
	}
	return 0
}

func TestParsePageAction(t *testing.T) {
	tests := []struct {
		name     string
		action   ViewerAction
		maxPage  int
		initPage int
		wantPage int
	}{
		{
			name:     "first page",
			action:   ViewerFirstPage,
			maxPage:  5,
			initPage: 3,
			wantPage: 0,
		},
		{
			name:     "previous page",
			action:   ViewerPrevPage,
			maxPage:  5,
			initPage: 3,
			wantPage: 2,
		},
		{
			name:     "next page",
			action:   ViewerNextPage,
			maxPage:  5,
			initPage: 3,
			wantPage: 4,
		},
		{
			name:     "last page",
			action:   ViewerLastPage,
			maxPage:  5,
			initPage: 3,
			wantPage: 5,
		},
		{
			name:     "prev page at start",
			action:   ViewerPrevPage,
			maxPage:  5,
			initPage: 0,
			wantPage: 0,
		},
		{
			name:     "next page at end",
			action:   ViewerNextPage,
			maxPage:  5,
			initPage: 5,
			wantPage: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newMockSession()
			s.Set(constants.SessionKeyPaginationPage, tt.initPage)

			var h ViewerAction
			got := h.ParsePageAction(s, tt.action, tt.maxPage)
			assert.Equal(t, tt.wantPage, got)

			// Verify session was updated
			assert.Equal(t, tt.wantPage, s.GetInt(constants.SessionKeyPaginationPage))
		})
	}
}
