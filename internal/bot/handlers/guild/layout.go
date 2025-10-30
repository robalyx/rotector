package guild

import (
	"fmt"

	"github.com/diamondburned/arikawa/v3/state"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/discord"
	"github.com/robalyx/rotector/internal/discord/verification"
	"github.com/robalyx/rotector/internal/redis"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the guild menu.
type Layout struct {
	db                  database.Client
	clients             []*state.State
	ratelimit           rueidis.Client
	scannerPool         *discord.ScannerPool
	verificationManager *verification.ServiceManager
	menu                *Menu
	scan                *ScanMenu
	logs                *LogsMenu
	lookup              *LookupMenu
	messages            *MessagesMenu
	logger              *zap.Logger
}

// New creates a Layout by initializing the guild menu.
func New(app *setup.App, clients []*state.State, verificationManager *verification.ServiceManager) *Layout {
	ratelimit, err := app.RedisManager.GetClient(redis.RatelimitDBIndex)
	if err != nil {
		app.Logger.Fatal("Failed to get Redis client for rate limiting", zap.Error(err))
	}

	messageAnalyzer := ai.NewMessageAnalyzer(app, app.Logger)

	// Create scanners for each client
	scanners := make([]*discord.Scanner, 0, len(clients))
	for i, client := range clients {
		scannerID := fmt.Sprintf("scanner_%d", i)
		scanner := discord.NewScanner(app.DB, app.CFClient, ratelimit, client.Session, messageAnalyzer, scannerID, app.Logger)
		scanners = append(scanners, scanner)
	}

	// Create scanner pool
	scannerPool := discord.NewScannerPool(scanners, app.DB, app.Logger)

	l := &Layout{
		db:                  app.DB,
		clients:             clients,
		ratelimit:           ratelimit,
		scannerPool:         scannerPool,
		verificationManager: verificationManager,
		logger:              app.Logger.Named("guild_menu"),
	}

	l.menu = NewMenu(l)
	l.scan = NewScanMenu(l)
	l.logs = NewLogsMenu(l)
	l.lookup = NewLookupMenu(l)
	l.messages = NewMessagesMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
		l.menu.page,
		l.scan.page,
		l.logs.page,
		l.lookup.page,
		l.messages.page,
	}
}

// getNextScanner returns the next scanner in round-robin fashion for load distribution.
func (l *Layout) getNextScanner() *discord.Scanner {
	scanner, _ := l.scannerPool.GetNext()
	return scanner
}
