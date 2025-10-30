package verification

import (
	"context"
	"errors"
)

var (
	ErrUserNotVerified               = errors.New("user not verified")
	ErrResponseTimeout               = errors.New("response timeout")
	ErrInvalidRobloxID               = errors.New("invalid roblox id format")
	ErrMissingNested                 = errors.New("missing nested components")
	ErrInvalidFormat                 = errors.New("invalid component format")
	ErrMissingContent                = errors.New("missing content field")
	ErrMissingFields                 = errors.New("missing required fields")
	ErrCommandNotFound               = errors.New("command not found in guild")
	ErrResponseNotFound              = errors.New("verification response not found in recent messages")
	ErrServiceTemporarilyUnavailable = errors.New("verification service temporarily unavailable")
	ErrUnexpectedResponseFormat      = errors.New("unexpected response format")
)

// Service defines the interface for verification services.
type Service interface {
	// ExecuteCommand executes the verification command for a Discord user.
	ExecuteCommand(ctx context.Context, discordUserID uint64) (*Response, error)
	// ParseResponse extracts Roblox information from the verification response.
	ParseResponse(response *Response) (int64, string, error)
	// GetServiceName returns a generic name for logging purposes.
	GetServiceName() string
	// Close performs cleanup when the service is no longer needed.
	Close() error
}

// Response represents a verification command response message.
type Response struct {
	ID         string           `json:"id"`
	Content    string           `json:"content"`
	Components []map[string]any `json:"components"`
	Embeds     []map[string]any `json:"embeds"`
}

// Config contains configuration for a verification service.
type Config struct {
	Token       string
	GuildID     uint64
	ChannelID   uint64
	CommandName string
	ServiceName string
}
