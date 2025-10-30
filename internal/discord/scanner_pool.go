package discord

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

var ErrNoScannersAvailable = errors.New("no scanners available in pool")

// ScannerPool manages a pool of scanners with round-robin load distribution.
type ScannerPool struct {
	scanners []*Scanner
	index    atomic.Uint32
	db       database.Client
	logger   *zap.Logger
}

// NewScannerPool creates a new scanner pool.
func NewScannerPool(scanners []*Scanner, db database.Client, logger *zap.Logger) *ScannerPool {
	return &ScannerPool{
		scanners: scanners,
		db:       db,
		logger:   logger.Named("scanner_pool"),
	}
}

// GetNext returns the next scanner using round-robin selection.
// Returns the scanner and its index. Returns (nil, -1) if pool is empty.
func (p *ScannerPool) GetNext() (*Scanner, int) {
	if len(p.scanners) == 0 {
		return nil, -1
	}

	// Use atomic operation for thread-safe round-robin with natural wraparound
	index := p.index.Add(1) - 1
	accountIndex := int(index) % len(p.scanners)

	return p.scanners[accountIndex], accountIndex
}

// GetAll returns all scanners in the pool.
func (p *ScannerPool) GetAll() []*Scanner {
	return p.scanners
}

// Size returns the number of scanners in the pool.
func (p *ScannerPool) Size() int {
	return len(p.scanners)
}

// ProcessConnections deduplicates and processes all discovered Roblox connections for a Discord user.
func (p *ScannerPool) ProcessConnections(ctx context.Context, userID uint64, allConnections []*types.DiscordRobloxConnection) error {
	if len(allConnections) == 0 {
		return nil
	}

	// Deduplicate connections by Roblox user ID
	uniqueConnections := make(map[int64]*types.DiscordRobloxConnection)

	for _, conn := range allConnections {
		if _, exists := uniqueConnections[conn.RobloxUserID]; !exists {
			uniqueConnections[conn.RobloxUserID] = conn
		}
	}

	p.logger.Info("Processing connections",
		zap.Uint64("userID", userID),
		zap.Int("total_connections", len(allConnections)),
		zap.Int("unique_connections", len(uniqueConnections)))

	// Fetch guild IDs for this user
	guildIDs, err := p.db.Model().Sync().GetDiscordUserGuilds(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to fetch user guilds for connection processing: %w", err)
	}

	// Get a scanner for processing
	scanner, _ := p.GetNext()
	if scanner == nil {
		return ErrNoScannersAvailable
	}

	// Process each unique connection
	for _, connection := range uniqueConnections {
		scanner.processRobloxConnection(ctx, userID, connection, guildIDs)
	}

	return nil
}
