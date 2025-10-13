package manager

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/robalyx/rotector/internal/cloudflare/api"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// ErrZoneNotFound is returned when a zone is not found in the database.
var ErrZoneNotFound = errors.New("zone not found")

// WarMapData represents the complete war map state for visualization.
type WarMapData struct {
	Zones         []WarZone      `json:"zones"`
	ActiveTargets []ActiveTarget `json:"activeTargets"`
	MajorOrders   []MajorOrder   `json:"majorOrders"`
	GlobalStats   GlobalStats    `json:"globalStats"`
	LastUpdated   time.Time      `json:"lastUpdated"`
}

// WarZone represents a zone in the war map.
type WarZone struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Liberation     float64   `json:"liberation"`     // 0-100%
	UserPercentage float64   `json:"userPercentage"` // Percentage of total users in this zone
	TotalUsers     int64     `json:"totalUsers"`
	BannedUsers    int64     `json:"bannedUsers"`
	FlaggedUsers   int64     `json:"flaggedUsers"`
	ConfirmedUsers int64     `json:"confirmedUsers"`
	IsActive       bool      `json:"isActive"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// MajorOrder represents a global objective.
type MajorOrder struct {
	ID                int64      `json:"id"`
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	Type              string     `json:"type"`
	TargetValue       int64      `json:"targetValue"`
	CurrentValue      int64      `json:"currentValue"`
	StartValue        int64      `json:"startValue"`
	Progress          float64    `json:"progress"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	CompletedAt       *time.Time `json:"completedAt"`
	RewardType        string     `json:"rewardType"`
	RewardDescription string     `json:"rewardDescription"`
	IsActive          bool       `json:"isActive"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// ActiveTarget represents a user being actively targeted for bans.
type ActiveTarget struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"userId"`
	UserName    string    `json:"userName"`
	UserStatus  int       `json:"userStatus"`
	Confidence  float64   `json:"confidence"`
	AssignedAt  time.Time `json:"assignedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	BanAttempts int       `json:"banAttempts"`
}

// GlobalStats represents overall war statistics.
type GlobalStats struct {
	TotalZones         int     `json:"totalZones"`
	TotalTargets       int     `json:"totalTargets"`
	TotalBanned        int64   `json:"totalBanned"`
	TotalFlagged       int64   `json:"totalFlagged"`
	TotalConfirmed     int64   `json:"totalConfirmed"`
	AverageLiberaction float64 `json:"averageLiberation"`
	ActiveMajorOrders  int     `json:"activeMajorOrders"`
}

// ZoneDetailsData represents detailed statistics and history for a specific zone.
type ZoneDetailsData struct {
	Zone              WarZone                  `json:"zone"`
	LiberationHistory []LiberationHistoryEntry `json:"liberationHistory"`
}

// LiberationHistoryEntry represents a single point in the liberation history.
type LiberationHistoryEntry struct {
	Date       string  `json:"date"`
	Liberation float64 `json:"liberation"`
}

// WarData manages war map data in D1 storage.
type WarData struct {
	d1     *api.D1Client
	r2     *api.R2Client
	logger *zap.Logger
}

// NewWarData creates a new war data manager.
func NewWarData(d1Client *api.D1Client, r2Client *api.R2Client, logger *zap.Logger) *WarData {
	return &WarData{
		d1:     d1Client,
		r2:     r2Client,
		logger: logger,
	}
}

// GetWarMapData retrieves the complete war map state.
func (w *WarData) GetWarMapData(ctx context.Context) (*WarMapData, error) {
	warData := &WarMapData{
		LastUpdated: time.Now().UTC(),
	}

	// Get war zones
	zones, err := w.getWarZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get war zones: %w", err)
	}

	warData.Zones = zones

	// Get global active targets
	targets, err := w.getGlobalActiveTargets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get global active targets: %w", err)
	}

	warData.ActiveTargets = targets

	// Get major orders
	orders, err := w.getMajorOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get major orders: %w", err)
	}

	warData.MajorOrders = orders

	// Get global stats
	stats, err := w.GetGlobalStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get global stats: %w", err)
	}

	warData.GlobalStats = stats

	return warData, nil
}

// SaveWarMapData saves the war map data to R2 for API consumption.
func (w *WarData) SaveWarMapData(ctx context.Context) error {
	// Fetch war map data
	data, err := w.GetWarMapData(ctx)
	if err != nil {
		return fmt.Errorf("failed to get war map data: %w", err)
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal war map data: %w", err)
	}

	// Save as latest version
	key := "war/map/latest.json"
	if err := w.r2.PutObject(ctx, key, jsonData, "application/json"); err != nil {
		return fmt.Errorf("failed to save war map data to R2: %w", err)
	}

	w.logger.Info("Saved war map data to R2",
		zap.String("key", key),
		zap.Int("zones", len(data.Zones)),
		zap.Int("majorOrders", len(data.MajorOrders)))

	return nil
}

// GetZoneDetailsData builds complete zone details including history and statistics.
func (w *WarData) GetZoneDetailsData(ctx context.Context, zoneID int64) (*ZoneDetailsData, error) {
	zoneDetails := &ZoneDetailsData{}

	// Get zone information
	sql := `
		SELECT id, liberation, total_users, banned_users, flagged_users, confirmed_users,
		       is_active, created_at, updated_at
		FROM war_zones
		WHERE id = ?
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, []any{zoneID})
	if err != nil {
		return nil, fmt.Errorf("failed to get zone %d: %w", zoneID, err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrZoneNotFound, zoneID)
	}

	row := results[0]

	createdAt, err := time.Parse("2006-01-02 15:04:05", row["created_at"].(string))
	if err != nil {
		w.logger.Warn("Failed to parse zone created_at", zap.Error(err))

		createdAt = time.Now().UTC()
	}

	updatedAt, err := time.Parse("2006-01-02 15:04:05", row["updated_at"].(string))
	if err != nil {
		w.logger.Warn("Failed to parse zone updated_at", zap.Error(err))

		updatedAt = time.Now().UTC()
	}

	zoneDetails.Zone = WarZone{
		ID:             zoneID,
		Name:           enum.UserCategoryType(zoneID).String(),
		Liberation:     row["liberation"].(float64),
		TotalUsers:     int64(row["total_users"].(float64)),
		BannedUsers:    int64(row["banned_users"].(float64)),
		FlaggedUsers:   int64(row["flagged_users"].(float64)),
		ConfirmedUsers: int64(row["confirmed_users"].(float64)),
		IsActive:       row["is_active"].(float64) == 1,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}

	// Get liberation history
	history, err := w.getZoneLiberationHistory(ctx, zoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to get liberation history for zone %d: %w", zoneID, err)
	}

	zoneDetails.LiberationHistory = history

	return zoneDetails, nil
}

// SaveZoneDetailsData saves zone details data to R2 for API consumption.
func (w *WarData) SaveZoneDetailsData(ctx context.Context, zoneID int64, data *ZoneDetailsData) error {
	// Serialize to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal zone details data: %w", err)
	}

	// Save to zone-specific file
	key := fmt.Sprintf("war/zones/zone-%d.json", zoneID)
	if err := w.r2.PutObject(ctx, key, jsonData, "application/json"); err != nil {
		return fmt.Errorf("failed to save zone details data to R2: %w", err)
	}

	w.logger.Info("Saved zone details data to R2",
		zap.String("key", key),
		zap.Int64("zoneID", zoneID),
		zap.Int("historyEntries", len(data.LiberationHistory)))

	return nil
}

// GetGlobalStats calculates overall war statistics.
func (w *WarData) GetGlobalStats(ctx context.Context) (GlobalStats, error) {
	// Get zone counts
	zoneCountSQL := `SELECT COUNT(*) as count FROM war_zones WHERE is_active = 1`

	zoneResults, err := w.d1.ExecuteSQL(ctx, zoneCountSQL, nil)
	if err != nil {
		return GlobalStats{}, err
	}

	// Get target counts
	targetCountSQL := `SELECT COUNT(*) as count FROM active_targets WHERE is_active = 1`

	targetResults, err := w.d1.ExecuteSQL(ctx, targetCountSQL, nil)
	if err != nil {
		return GlobalStats{}, err
	}

	// Get user totals
	userStatsSQL := `
		SELECT 
			SUM(total_users) as total_users,
			SUM(banned_users) as banned_users,
			SUM(flagged_users) as flagged_users,
			SUM(confirmed_users) as confirmed_users,
			AVG(liberation) as avg_liberation
		FROM war_zones WHERE is_active = 1
	`

	userResults, err := w.d1.ExecuteSQL(ctx, userStatsSQL, nil)
	if err != nil {
		return GlobalStats{}, err
	}

	// Get major order counts
	orderCountSQL := `SELECT COUNT(*) as count FROM major_orders WHERE is_active = 1 AND expires_at > datetime('now')`

	orderResults, err := w.d1.ExecuteSQL(ctx, orderCountSQL, nil)
	if err != nil {
		return GlobalStats{}, err
	}

	stats := GlobalStats{
		TotalZones:        int(zoneResults[0]["count"].(float64)),
		TotalTargets:      int(targetResults[0]["count"].(float64)),
		ActiveMajorOrders: int(orderResults[0]["count"].(float64)),
	}

	if len(userResults) > 0 && userResults[0]["banned_users"] != nil {
		stats.TotalBanned = int64(userResults[0]["banned_users"].(float64))
		stats.TotalFlagged = int64(userResults[0]["flagged_users"].(float64))
		stats.TotalConfirmed = int64(userResults[0]["confirmed_users"].(float64))
		stats.AverageLiberaction = userResults[0]["avg_liberation"].(float64)
	}

	return stats, nil
}

// getWarZones retrieves all war zones.
func (w *WarData) getWarZones(ctx context.Context) ([]WarZone, error) {
	sql := `
		SELECT z.id, z.liberation,
		       z.total_users, z.banned_users, z.flagged_users, z.confirmed_users,
		       z.is_active, z.created_at, z.updated_at
		FROM war_zones z
		WHERE z.is_active = 1
		ORDER BY z.id
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, err
	}

	zones := make([]WarZone, 0, len(results))
	for _, row := range results {
		createdAt, err := time.Parse("2006-01-02 15:04:05", row["created_at"].(string))
		if err != nil {
			w.logger.Warn("Failed to parse zone created_at", zap.Error(err))

			createdAt = time.Now().UTC()
		}

		updatedAt, err := time.Parse("2006-01-02 15:04:05", row["updated_at"].(string))
		if err != nil {
			w.logger.Warn("Failed to parse zone updated_at", zap.Error(err))

			updatedAt = time.Now().UTC()
		}

		zoneID := int64(row["id"].(float64))

		zone := WarZone{
			ID:             zoneID,
			Name:           enum.UserCategoryType(zoneID).String(),
			Liberation:     row["liberation"].(float64),
			TotalUsers:     int64(row["total_users"].(float64)),
			BannedUsers:    int64(row["banned_users"].(float64)),
			FlaggedUsers:   int64(row["flagged_users"].(float64)),
			ConfirmedUsers: int64(row["confirmed_users"].(float64)),
			IsActive:       row["is_active"].(float64) == 1,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		}

		zones = append(zones, zone)
	}

	// Calculate user percentages across all zones
	var totalUsers int64
	for i := range zones {
		totalUsers += zones[i].TotalUsers
	}

	if totalUsers > 0 {
		for i := range zones {
			zones[i].UserPercentage = float64(zones[i].TotalUsers) / float64(totalUsers) * 100
		}
	}

	return zones, nil
}

// getGlobalActiveTargets retrieves all active targets globally.
func (w *WarData) getGlobalActiveTargets(ctx context.Context) ([]ActiveTarget, error) {
	sql := `
		SELECT at.id, at.user_id, at.user_name, at.user_status, at.confidence,
		       at.assigned_at, at.expires_at, at.ban_attempts
		FROM active_targets at
		WHERE at.is_active = 1
		ORDER BY at.assigned_at ASC
		LIMIT 6
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, err
	}

	targets := make([]ActiveTarget, 0, len(results))
	for _, row := range results {
		assignedAt, _ := time.Parse("2006-01-02 15:04:05", row["assigned_at"].(string))
		expiresAt, _ := time.Parse("2006-01-02 15:04:05", row["expires_at"].(string))

		target := ActiveTarget{
			ID:          int64(row["id"].(float64)),
			UserID:      int64(row["user_id"].(float64)),
			UserName:    row["user_name"].(string),
			UserStatus:  int(row["user_status"].(float64)),
			Confidence:  row["confidence"].(float64),
			AssignedAt:  assignedAt,
			ExpiresAt:   expiresAt,
			BanAttempts: int(row["ban_attempts"].(float64)),
		}
		targets = append(targets, target)
	}

	return targets, nil
}

// getMajorOrders retrieves active major orders.
func (w *WarData) getMajorOrders(ctx context.Context) ([]MajorOrder, error) {
	sql := `
		SELECT id, title, description, type, target_value, current_value, start_value,
		       expires_at, completed_at, reward_type, reward_description, is_active,
		       created_at, updated_at
		FROM major_orders
		WHERE is_active = 1 AND expires_at > datetime('now')
		ORDER BY id DESC
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, err
	}

	orders := make([]MajorOrder, 0, len(results))
	for _, row := range results {
		expiresAt, err := time.Parse("2006-01-02 15:04:05", row["expires_at"].(string))
		if err != nil {
			w.logger.Warn("Failed to parse expires_at", zap.Error(err))

			expiresAt = time.Now().Add(7 * 24 * time.Hour) // Default to 7 days from now
		}

		var completedAt *time.Time
		if row["completed_at"] != nil {
			t, err := time.Parse("2006-01-02 15:04:05", row["completed_at"].(string))
			if err != nil {
				w.logger.Warn("Failed to parse completed_at", zap.Error(err))
			} else {
				completedAt = &t
			}
		}

		createdAt, err := time.Parse("2006-01-02 15:04:05", row["created_at"].(string))
		if err != nil {
			w.logger.Warn("Failed to parse major order created_at", zap.Error(err))

			createdAt = time.Now().UTC()
		}

		updatedAt, err := time.Parse("2006-01-02 15:04:05", row["updated_at"].(string))
		if err != nil {
			w.logger.Warn("Failed to parse major order updated_at", zap.Error(err))

			updatedAt = time.Now().UTC()
		}

		targetValue := int64(row["target_value"].(float64))
		currentValue := int64(row["current_value"].(float64))
		startValue := int64(row["start_value"].(float64))

		var progress float64
		if targetValue > startValue {
			progress = float64(currentValue-startValue) / float64(targetValue-startValue) * 100
		}

		order := MajorOrder{
			ID:                int64(row["id"].(float64)),
			Title:             row["title"].(string),
			Description:       row["description"].(string),
			Type:              row["type"].(string),
			TargetValue:       targetValue,
			CurrentValue:      currentValue,
			StartValue:        startValue,
			Progress:          progress,
			ExpiresAt:         expiresAt,
			CompletedAt:       completedAt,
			RewardType:        row["reward_type"].(string),
			RewardDescription: row["reward_description"].(string),
			IsActive:          row["is_active"].(float64) == 1,
			CreatedAt:         createdAt,
			UpdatedAt:         updatedAt,
		}
		orders = append(orders, order)
	}

	return orders, nil
}

// getZoneLiberationHistory retrieves 7-day historical liberation data for a zone.
func (w *WarData) getZoneLiberationHistory(ctx context.Context, zoneID int64) ([]LiberationHistoryEntry, error) {
	sql := `
		SELECT stat_value, recorded_at
		FROM war_stats
		WHERE stat_type = 'zone_liberation'
		  AND stat_key = ?
		  AND recorded_at >= datetime('now', '-7 days')
		ORDER BY recorded_at ASC
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, []any{strconv.FormatInt(zoneID, 10)})
	if err != nil {
		return nil, fmt.Errorf("failed to get liberation history for zone %d: %w", zoneID, err)
	}

	// Group by date and take most recent value per day
	dateMap := make(map[string]float64)
	for _, row := range results {
		recordedAt, err := time.Parse("2006-01-02 15:04:05", row["recorded_at"].(string))
		if err != nil {
			w.logger.Warn("Failed to parse recorded_at for liberation history", zap.Error(err))

			continue
		}

		date := recordedAt.Format("2006-01-02")
		liberation := row["stat_value"].(float64)

		// Keep the most recent value for each date
		dateMap[date] = liberation
	}

	// Convert map to sorted array
	history := make([]LiberationHistoryEntry, 0, len(dateMap))
	for date, liberation := range dateMap {
		history = append(history, LiberationHistoryEntry{
			Date:       date,
			Liberation: liberation,
		})
	}

	// Sort by date ascending
	slices.SortFunc(history, func(a, b LiberationHistoryEntry) int {
		return cmp.Compare(a.Date, b.Date)
	})

	return history, nil
}
