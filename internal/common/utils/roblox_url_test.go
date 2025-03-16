package utils_test

import (
	"testing"

	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/stretchr/testify/assert"
)

func TestIsRobloxProfileURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "valid HTTPS URL with profile",
			input: "https://www.roblox.com/users/123456789/profile",
			want:  true,
		},
		{
			name:  "valid HTTP URL with profile",
			input: "http://www.roblox.com/users/123456789/profile",
			want:  true,
		},
		{
			name:  "valid URL without www with profile",
			input: "https://roblox.com/users/123456789/profile",
			want:  true,
		},
		{
			name:  "valid URL without protocol",
			input: "roblox.com/users/123456789/profile",
			want:  true,
		},
		{
			name:  "valid URL without profile suffix",
			input: "roblox.com/users/123456789",
			want:  true,
		},
		{
			name:  "invalid URL - singular user",
			input: "roblox.com/user/123456789",
			want:  false,
		},
		{
			name:  "invalid URL - wrong domain",
			input: "https://example.com/users/123456789/profile",
			want:  false,
		},
		{
			name:  "invalid URL - wrong path",
			input: "https://roblox.com/profiles/123456789",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "just ID",
			input: "123456789",
			want:  false,
		},
		{
			name:  "ID in message",
			input: "check out my profile: 123456789",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.IsRobloxProfileURL(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsRobloxGroupURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "valid HTTPS URL with group name",
			input: "https://www.roblox.com/groups/123456789/group-name",
			want:  true,
		},
		{
			name:  "valid HTTP URL with group name",
			input: "http://www.roblox.com/groups/123456789/group-name",
			want:  true,
		},
		{
			name:  "valid URL without www with group name",
			input: "https://roblox.com/groups/123456789/group-name",
			want:  true,
		},
		{
			name:  "valid URL without protocol",
			input: "roblox.com/groups/123456789/group-name",
			want:  true,
		},
		{
			name:  "valid URL without group name",
			input: "roblox.com/groups/123456789",
			want:  true,
		},
		{
			name:  "valid URL with communities",
			input: "roblox.com/communities/123456789/community-name",
			want:  true,
		},
		{
			name:  "valid URL with communities without name",
			input: "roblox.com/communities/123456789",
			want:  true,
		},
		{
			name:  "invalid URL - singular group",
			input: "roblox.com/group/123456789",
			want:  false,
		},
		{
			name:  "invalid URL - wrong domain",
			input: "https://example.com/groups/123456789/group-name",
			want:  false,
		},
		{
			name:  "invalid URL - wrong path format",
			input: "https://roblox.com/g/123456789",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "just ID",
			input: "123456789",
			want:  false,
		},
		{
			name:  "ID in message",
			input: "join my group: 123456789",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.IsRobloxGroupURL(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsRobloxGameURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "valid HTTPS URL with game name",
			input: "https://www.roblox.com/games/123456789/game-name",
			want:  true,
		},
		{
			name:  "valid HTTP URL with game name",
			input: "http://www.roblox.com/games/123456789/game-name",
			want:  true,
		},
		{
			name:  "valid URL without www with game name",
			input: "https://roblox.com/games/123456789/game-name",
			want:  true,
		},
		{
			name:  "valid URL without protocol",
			input: "roblox.com/games/123456789/game-name",
			want:  true,
		},
		{
			name:  "valid URL without game name",
			input: "roblox.com/games/123456789",
			want:  true,
		},
		{
			name:  "invalid URL - singular game",
			input: "roblox.com/game/123456789",
			want:  false,
		},
		{
			name:  "invalid URL - wrong domain",
			input: "https://example.com/games/123456789/game-name",
			want:  false,
		},
		{
			name:  "invalid URL - wrong path",
			input: "https://roblox.com/play/123456789",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "just ID",
			input: "123456789",
			want:  false,
		},
		{
			name:  "ID in message",
			input: "you guys should join my game: 123456789",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.IsRobloxGameURL(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractUserIDFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:    "valid URL with profile",
			input:   "https://www.roblox.com/users/123456789/profile",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without www",
			input:   "https://roblox.com/users/123456789/profile",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without protocol",
			input:   "roblox.com/users/123456789/profile",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without profile suffix",
			input:   "roblox.com/users/123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "just ID",
			input:   "123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "ID in message",
			input:   "check out my profile: 123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "invalid URL - singular user",
			input:   "roblox.com/user/123456789",
			want:    "",
			wantErr: utils.ErrInvalidProfileURL,
		},
		{
			name:    "invalid URL - wrong domain",
			input:   "https://example.com/users/123456789",
			want:    "",
			wantErr: utils.ErrInvalidProfileURL,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: utils.ErrInvalidProfileURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := utils.ExtractUserIDFromURL(tt.input)
			assert.Equal(t, tt.want, got)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestExtractGroupIDFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:    "valid URL with group name",
			input:   "https://www.roblox.com/groups/123456789/group-name",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without www",
			input:   "https://roblox.com/groups/123456789/group-name",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without protocol",
			input:   "roblox.com/groups/123456789/group-name",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without group name",
			input:   "roblox.com/groups/123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL with communities",
			input:   "roblox.com/communities/123456789/community-name",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL with communities without name",
			input:   "roblox.com/communities/123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "just ID",
			input:   "123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "ID in message",
			input:   "join my group: 123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "invalid URL - singular group",
			input:   "roblox.com/group/123456789",
			want:    "",
			wantErr: utils.ErrInvalidGroupURL,
		},
		{
			name:    "invalid URL - wrong domain",
			input:   "https://example.com/groups/123456789",
			want:    "",
			wantErr: utils.ErrInvalidGroupURL,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: utils.ErrInvalidGroupURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := utils.ExtractGroupIDFromURL(tt.input)
			assert.Equal(t, tt.want, got)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestExtractGameIDFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:    "valid URL with game name",
			input:   "https://www.roblox.com/games/123456789/game-name",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without www",
			input:   "https://roblox.com/games/123456789/game-name",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without protocol",
			input:   "roblox.com/games/123456789/game-name",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "valid URL without game name",
			input:   "roblox.com/games/123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "just ID",
			input:   "123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "ID in message",
			input:   "you guys should join my game: 123456789",
			want:    "123456789",
			wantErr: nil,
		},
		{
			name:    "invalid URL - singular game",
			input:   "roblox.com/game/123456789",
			want:    "",
			wantErr: utils.ErrInvalidGameURL,
		},
		{
			name:    "invalid URL - wrong domain",
			input:   "https://example.com/games/123456789",
			want:    "",
			wantErr: utils.ErrInvalidGameURL,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: utils.ErrInvalidGameURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := utils.ExtractGameIDFromURL(tt.input)
			assert.Equal(t, tt.want, got)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
