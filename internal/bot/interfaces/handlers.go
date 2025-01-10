package interfaces

import (
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// DashboardLayout defines the interface for handling dashboard-related actions.
type DashboardLayout interface {
	// Show prepares and displays the dashboard menu.
	Show(event CommonEvent, s *session.Session, content string)
}

// UserReviewLayout defines the interface for handling user review-related actions.
type UserReviewLayout interface {
	// ShowReviewMenu prepares and displays the review menu.
	ShowReviewMenu(event CommonEvent, s *session.Session)
	// ShowStatusMenu prepares and displays the status menu.
	ShowStatusMenu(event CommonEvent, s *session.Session)
}

// GroupReviewLayout defines the interface for handling group review-related actions.
type GroupReviewLayout interface {
	// Show prepares and displays the group review menu.
	Show(event CommonEvent, s *session.Session)
}

// SettingLayout defines the interface for handling settings-related actions.
type SettingLayout interface {
	// ShowUser prepares and displays the user settings menu.
	ShowUser(event CommonEvent, s *session.Session)
	// ShowBot prepares and displays the bot settings menu.
	ShowBot(event CommonEvent, s *session.Session)
	// ShowUpdate prepares and displays a setting update menu.
	ShowUpdate(event CommonEvent, s *session.Session, prefix string, option string)
}

// LogLayout defines the interface for handling logs-related actions.
type LogLayout interface {
	// Show prepares and displays the log menu.
	Show(event CommonEvent, s *session.Session)
	// ResetLogs clears the logs from the session.
	ResetLogs(s *session.Session)
	// ResetFilters resets all log filters to their default values in the given session.
	ResetFilters(s *session.Session)
}

// QueueLayout defines the interface for handling queue-related actions.
type QueueLayout interface {
	// Show prepares and displays the queue menu.
	Show(event CommonEvent, s *session.Session)
}

// ChatLayout defines the interface for handling AI chat-related actions.
type ChatLayout interface {
	// Show prepares and displays the chat menu.
	Show(event CommonEvent, s *session.Session)
}

// AppealLayout defines the interface for handling appeal-related actions.
type AppealLayout interface {
	// ShowOverview displays the appeal overview menu.
	ShowOverview(event CommonEvent, s *session.Session, content string)
	// ShowTicket displays a specific appeal ticket.
	ShowTicket(event CommonEvent, s *session.Session, appealID int64, content string)
}

// CaptchaLayout defines the interface for CAPTCHA verification.
type CaptchaLayout interface {
	Show(event CommonEvent, s *session.Session, content string)
}

// AdminLayout defines the interface for handling admin-related actions.
type AdminLayout interface {
	// Show prepares and displays the admin menu.
	Show(event CommonEvent, s *session.Session)
}

// BanLayout defines the interface for handling ban-related displays.
type BanLayout interface {
	// Show prepares and displays the ban information menu.
	Show(event CommonEvent, s *session.Session)
}
