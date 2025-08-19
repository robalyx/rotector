package export

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytedance/sonic"
	dbTypes "github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/export/binary"
	"github.com/robalyx/rotector/internal/export/csv"
	"github.com/robalyx/rotector/internal/export/sqlite"
	"github.com/robalyx/rotector/internal/export/types"
	"github.com/robalyx/rotector/internal/setup"
)

var ErrUnsupportedFormat = errors.New("unsupported export format")

// Format represents a supported export format.
type Format string

const (
	FormatSQLite Format = "sqlite"
	FormatBinary Format = "binary"
	FormatCSV    Format = "csv"
)

const (
	// EngineVersion represents the version of the export engine.
	// This should be updated when making breaking changes to the export format.
	EngineVersion = "2.0.0"
)

// Config holds the configuration for exports.
type Config struct {
	ExportVersion string `json:"exportVersion"`
	Salt          string `json:"salt"`
	Description   string `json:"description"`
	HashType      string `json:"hashType"`
	Iterations    uint32 `json:"iterations"`
	Memory        uint32 `json:"memory,omitempty"`
	Concurrency   int64  `json:"-"`
}

// Exporter handles exporting flagged users and groups.
type Exporter struct {
	app     *setup.App
	outDir  string
	config  *Config
	formats []Format
}

// New creates a new exporter instance.
func New(app *setup.App, outDir string, config *Config) *Exporter {
	return &Exporter{
		app:    app,
		outDir: outDir,
		config: config,
		formats: []Format{
			FormatSQLite,
			FormatBinary,
			FormatCSV,
		},
	}
}

// ExportAll exports all data in all supported formats.
func (e *Exporter) ExportAll(ctx context.Context) error {
	// Print export configuration
	fmt.Printf("Starting export with configuration:\n")
	fmt.Printf("  Hash Type: %s\n", e.config.HashType)
	fmt.Printf("  Concurrency: %d workers\n", e.config.Concurrency)
	fmt.Printf("  Iterations: %d\n", e.config.Iterations)

	if e.config.HashType == string(HashTypeArgon2id) {
		fmt.Printf("  Memory: %d MB\n", e.config.Memory)
	}

	fmt.Printf("  Output Directory: %s\n", e.outDir)
	fmt.Printf("  Export Version: %s\n", e.config.ExportVersion)
	fmt.Printf("  Engine Version: %s\n", EngineVersion)
	fmt.Printf("  Description: %s\n\n", e.config.Description)

	// Get all flagged and confirmed users and groups
	fmt.Printf("Fetching data from database...\n")

	users, groups, err := e.getFlaggedData(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Found %d users and %d groups to export\n\n", len(users), len(groups))

	// Convert to export records
	fmt.Printf("Hashing user IDs...\n")

	userRecords := e.hashRecords(users, e.config.Salt, HashType(e.config.HashType))

	fmt.Printf("\nHashing group IDs...\n")

	groupRecords := e.hashRecords(groups, e.config.Salt, HashType(e.config.HashType))

	fmt.Printf("\nCompleted hashing all records\n\n")

	// Save config file
	fmt.Printf("Saving export configuration...\n")

	configPath := filepath.Join(e.outDir, "export_config.json")

	// Create config with engine version for JSON
	jsonConfig := struct {
		*Config

		EngineVersion string `json:"engineVersion"`
	}{
		Config:        e.config,
		EngineVersion: EngineVersion,
	}

	configData, err := sonic.MarshalIndent(jsonConfig, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal export config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		return fmt.Errorf("failed to write export config: %w", err)
	}

	// Export each format
	fmt.Printf("Exporting data in %d formats...\n", len(e.formats))

	for _, format := range e.formats {
		fmt.Printf("  Writing %s format...\n", format)

		if err := e.export(format, userRecords, groupRecords); err != nil {
			return fmt.Errorf("failed to export %s format: %w", format, err)
		}
	}

	fmt.Printf("\nExport completed successfully\n")
	fmt.Printf("Files written to: %s\n", e.outDir)

	return nil
}

// hashRecords converts items to export records with concurrent hashing.
func (e *Exporter) hashRecords(items any, salt string, hashType HashType) []*types.ExportRecord {
	var (
		ids         []int64
		reasons     []string
		statuses    []string
		confidences []float64
	)

	// Extract data based on type

	switch v := items.(type) {
	case []*dbTypes.ReviewUser:
		ids = make([]int64, len(v))
		reasons = make([]string, len(v))
		statuses = make([]string, len(v))

		confidences = make([]float64, len(v))
		for i, user := range v {
			ids[i] = user.ID
			reasons[i] = strings.Join(user.Reasons.Messages(), "; ")
			statuses[i] = user.Status.String()
			confidences[i] = user.Confidence
		}
	case []*dbTypes.ReviewGroup:
		ids = make([]int64, len(v))
		reasons = make([]string, len(v))
		statuses = make([]string, len(v))

		confidences = make([]float64, len(v))
		for i, group := range v {
			ids[i] = group.ID
			reasons[i] = strings.Join(group.Reasons.Messages(), "; ")
			statuses[i] = group.Status.String()
			confidences[i] = group.Confidence
		}
	}

	// Hash IDs concurrently
	hashes := hashIDs(ids, salt, hashType, e.config.Concurrency, e.config.Iterations, e.config.Memory)

	// Create records
	records := make([]*types.ExportRecord, len(ids))
	for i := range ids {
		records[i] = &types.ExportRecord{
			Hash:       hashes[i],
			Status:     statuses[i],
			Reason:     reasons[i],
			Confidence: confidences[i],
		}
	}

	return records
}

// getFlaggedData retrieves all flagged and confirmed users and groups from the database.
func (e *Exporter) getFlaggedData(
	ctx context.Context,
) (users []*dbTypes.ReviewUser, groups []*dbTypes.ReviewGroup, err error) {
	users, err = e.app.DB.Model().User().GetFlaggedAndConfirmedUsers(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get users: %w", err)
	}

	groups, err = e.app.DB.Model().Group().GetFlaggedAndConfirmedGroups(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get groups: %w", err)
	}

	return users, groups, nil
}

// export handles exporting data in the specified format.
func (e *Exporter) export(format Format, userRecords, groupRecords []*types.ExportRecord) error {
	var exporter interface {
		Export(userRecords, groupRecords []*types.ExportRecord) error
	}

	switch format {
	case FormatSQLite:
		exporter = sqlite.New(e.outDir)
	case FormatBinary:
		exporter = binary.New(e.outDir)
	case FormatCSV:
		exporter = csv.New(e.outDir)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}

	return exporter.Export(userRecords, groupRecords)
}
