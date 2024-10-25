package interfaces

import (
	"github.com/rotector/rotector/internal/bot/session"
)

// DashboardHandler defines the interface for handling dashboard-related actions.
type DashboardHandler interface {
	ShowDashboard(event CommonEvent)
}

// ReviewHandler defines the interface for handling review-related actions.
type ReviewHandler interface {
	ShowReviewMenuAndFetchUser(event CommonEvent, s *session.Session, content string)
}

// SettingsHandler defines the interface for handling settings-related actions.
type SettingsHandler interface {
	ShowUserSettings(event CommonEvent, s *session.Session)
	ShowGuildSettings(event CommonEvent, s *session.Session)
}

// LogsHandler defines the interface for handling logs-related actions.
type LogsHandler interface {
	ShowLogMenu(event CommonEvent, s *session.Session)
}
