package manager

import (
	"context"
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/cloudflare/api"
	"go.uber.org/zap"
)

const (
	// maxStatsBatchSize limits records per INSERT to avoid parameter limit.
	maxStatsBatchSize = 25
)

// StatisticRecord represents a single statistic record to be inserted.
type StatisticRecord struct {
	StatType  string
	StatKey   string
	StatValue float64
}

// WarStats manages war statistics recording and retrieval.
type WarStats struct {
	d1     *api.D1Client
	logger *zap.Logger
}

// NewWarStats creates a new war statistics manager.
func NewWarStats(d1Client *api.D1Client, logger *zap.Logger) *WarStats {
	return &WarStats{
		d1:     d1Client,
		logger: logger,
	}
}

// RecordStatisticsBatch records multiple statistics using chunked batch inserts.
func (w *WarStats) RecordStatisticsBatch(ctx context.Context, records []StatisticRecord) error {
	if len(records) == 0 {
		return nil
	}

	for i := 0; i < len(records); i += maxStatsBatchSize {
		end := i + maxStatsBatchSize
		if end > len(records) {
			end = len(records)
		}

		chunk := records[i:end]

		// Build SQL with multiple VALUES clauses for this chunk
		placeholders := make([]string, 0, len(chunk))
		args := make([]any, 0, len(chunk)*3)

		for _, record := range chunk {
			placeholders = append(placeholders, "(?, ?, ?, datetime('now'))")
			args = append(args, record.StatType, record.StatKey, record.StatValue)
		}

		sql := fmt.Sprintf(`
			INSERT INTO war_stats (stat_type, stat_key, stat_value, recorded_at)
			VALUES %s
		`, strings.Join(placeholders, ", "))

		_, err := w.d1.ExecuteSQL(ctx, sql, args)
		if err != nil {
			return fmt.Errorf("failed to record statistics batch (chunk %d-%d): %w", i, end, err)
		}
	}

	return nil
}

// GetDailyExtensionReportCount gets the count of extension reports processed in the last 24 hours.
func (w *WarStats) GetDailyExtensionReportCount(ctx context.Context) (int64, error) {
	sql := `
		SELECT COUNT(*) as count 
		FROM extension_reports 
		WHERE processed_at > datetime('now', '-24 hours')
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get daily extension report count: %w", err)
	}

	if len(results) == 0 {
		return 0, nil
	}

	return int64(results[0]["count"].(float64)), nil
}

// GetZoneLiberationPercentage gets the current liberation percentage for a zone.
func (w *WarStats) GetZoneLiberationPercentage(ctx context.Context, zoneID int64) (float64, error) {
	sql := `SELECT liberation FROM war_zones WHERE id = ?`

	results, err := w.d1.ExecuteSQL(ctx, sql, []any{zoneID})
	if err != nil {
		return 0, fmt.Errorf("failed to get zone liberation: %w", err)
	}

	if len(results) == 0 {
		return 0, nil
	}

	return results[0]["liberation"].(float64), nil
}
