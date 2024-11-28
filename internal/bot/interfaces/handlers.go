package interfaces

import (
	"github.com/rotector/rotector/internal/bot/core/session"
)

// DashboardLayout defines the interface for handling dashboard-related actions.
type DashboardLayout interface {
	// Show prepares and displays the dashboard interface by loading
	// statistics and active user information into the session.
	Show(event CommonEvent, s *session.Session, content string)
}

// UserReviewLayout defines the interface for handling user review-related actions.
type UserReviewLayout interface {
	// ShowReviewMenu prepares and displays the review interface by loading
	// user data and friend status information into the session.
	ShowReviewMenu(event CommonEvent, s *session.Session)
	// ShowStatusMenu prepares and displays the status interface by loading
	// current queue counts and position information into the session.
	ShowStatusMenu(event CommonEvent, s *session.Session)
}

// GroupReviewLayout defines the interface for handling group review-related actions.
type GroupReviewLayout interface {
	// Show prepares and displays the review interface by loading
	// group data into the session.
	Show(event CommonEvent, s *session.Session)
}

// SettingLayout defines the interface for handling settings-related actions.
type SettingLayout interface {
	// ShowUser loads user settings from the database into the session and
	// displays them through the pagination system.
	ShowUser(event CommonEvent, s *session.Session)
	// ShowBot loads bot settings from the database into the session and
	// displays them through the pagination system.
	ShowBot(event CommonEvent, s *session.Session)
}

// LogLayout defines the interface for handling logs-related actions.
type LogLayout interface {
	// Show prepares and displays the log interface by initializing
	// session data with default values and loading user preferences.
	Show(event CommonEvent, s *session.Session)
	// ResetFilters resets all log filters to their default values in the given session.
	// This is useful when switching between different views or users.
	ResetFilters(s *session.Session)
}

// QueueLayout defines the interface for handling queue-related actions.
type QueueLayout interface {
	// Show prepares and displays the queue interface by loading
	// current queue lengths into the session.
	Show(event CommonEvent, s *session.Session)
}
