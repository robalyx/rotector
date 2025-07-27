package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/export"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/urfave/cli/v3"
)

const (
	// ExportLogDir specifies where export log files are stored.
	ExportLogDir = "logs/export_logs"
)

var ErrInvalidHashType = errors.New("invalid hash type")

func main() {
	if err := run(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	app := &cli.Command{
		Name:  "export",
		Usage: "Export flagged users and groups to various file formats",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "exports",
				Usage:   "Base output directory for export files",
			},
			&cli.StringFlag{
				Name:    "salt",
				Aliases: []string{"s"},
				Usage:   "Salt for hashing IDs",
			},
			&cli.StringFlag{
				Name:    "export-version",
				Aliases: []string{"v"},
				Usage:   "Export version",
			},
			&cli.StringFlag{
				Name:    "description",
				Aliases: []string{"d"},
				Usage:   "Export description",
			},
			&cli.StringFlag{
				Name:    "hash-type",
				Aliases: []string{"t"},
				Usage:   "Hash algorithm to use (argon2id or sha256)",
			},
			&cli.IntFlag{
				Name:    "concurrency",
				Aliases: []string{"c"},
				Usage:   "Number of concurrent hash operations",
				Value:   1,
			},
			&cli.UintFlag{
				Name:    "iterations",
				Aliases: []string{"i"},
				Usage:   "Number of hash iterations",
			},
			&cli.UintFlag{
				Name:    "memory",
				Aliases: []string{"m"},
				Usage:   "Memory to use for Argon2id in MB",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			// Initialize application with required dependencies
			app, err := setup.InitializeApp(ctx, setup.ServiceExport, ExportLogDir)
			if err != nil {
				return fmt.Errorf("failed to initialize application: %w", err)
			}
			defer app.Cleanup(ctx)

			// Create timestamped output directory
			baseDir := c.String("output")
			timestamp := time.Now().UTC().Format("2006-01-02_150405")
			outDir := filepath.Join(baseDir, timestamp)
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}

			// Get export configuration
			config, err := getExportConfig(c)
			if err != nil {
				return fmt.Errorf("failed to get export configuration: %w", err)
			}

			// Create exporter
			exporter := export.New(app, outDir, config)

			// Export all formats
			if err := exporter.ExportAll(ctx); err != nil {
				return fmt.Errorf("failed to export data: %w", err)
			}

			return nil
		},
	}

	return app.Run(context.Background(), os.Args)
}

// getExportConfig retrieves export configuration from CLI flags or interactive prompts.
func getExportConfig(c *cli.Command) (*export.Config, error) {
	config := &export.Config{
		ExportVersion: c.String("export-version"),
		Salt:          c.String("salt"),
		Description:   c.String("description"),
		HashType:      c.String("hash-type"),
		Concurrency:   c.Int64("concurrency"),
		Iterations:    uint32(c.Uint("iterations")), //nolint:gosec // -
		Memory:        uint32(c.Uint("memory")),     //nolint:gosec // -
	}

	reader := bufio.NewReader(os.Stdin)

	type field struct {
		name     string
		value    *string
		prompt   string
		defValue string
		validate func(string) error
	}

	fields := []field{
		{
			name:     "export version",
			value:    &config.ExportVersion,
			prompt:   "Enter export version",
			defValue: "1.0.0",
		},
		{
			name:   "salt",
			value:  &config.Salt,
			prompt: "Enter salt for hashing IDs",
		},
		{
			name:     "description",
			value:    &config.Description,
			prompt:   "Enter export description",
			defValue: "Rotector Export",
		},
		{
			name:     "hash type",
			value:    &config.HashType,
			prompt:   "Enter hash type (argon2id/sha256)",
			defValue: "sha256",
			validate: func(v string) error {
				if v != string(export.HashTypeArgon2id) && v != string(export.HashTypeSHA256) {
					return fmt.Errorf("%w: %s", ErrInvalidHashType, v)
				}
				return nil
			},
		},
	}

	// Handle string fields
	for _, f := range fields {
		if *f.value == "" {
			prompt := f.prompt
			if f.defValue != "" {
				prompt = fmt.Sprintf("%s [%s]", prompt, f.defValue)
			}

			val, err := promptString(reader, prompt)
			if err != nil {
				return nil, fmt.Errorf("failed to read %s: %w", f.name, err)
			}

			if val == "" && f.defValue != "" {
				val = f.defValue
			}

			if f.validate != nil {
				if err := f.validate(val); err != nil {
					return nil, err
				}
			}

			*f.value = val
		}
	}

	// Handle iterations
	if config.Iterations == 0 {
		defaultIter := "1"
		if config.HashType == string(export.HashTypeArgon2id) {
			defaultIter = "16"
		}

		iter, err := promptUint32(reader, "Enter hash iterations", defaultIter)
		if err != nil {
			return nil, fmt.Errorf("failed to read iterations: %w", err)
		}

		config.Iterations = iter
	}

	// Handle memory for Argon2id
	if config.Memory == 0 && config.HashType == string(export.HashTypeArgon2id) {
		mem, err := promptUint32(reader, "Enter memory usage in MB for Argon2id", "16")
		if err != nil {
			return nil, fmt.Errorf("failed to read memory: %w", err)
		}

		config.Memory = mem
	}

	return config, nil
}

// promptString prompts for a string value.
func promptString(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt + ": ")

	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(input), nil
}

// promptUint32 prompts for a uint32 value with a default.
func promptUint32(reader *bufio.Reader, prompt, defValue string) (uint32, error) {
	val, err := promptString(reader, prompt+" ["+defValue+"]")
	if err != nil {
		return 0, err
	}

	if val == "" {
		val = defValue
	}

	num, err := strconv.ParseUint(val, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %w", err)
	}

	return uint32(num), nil
}
