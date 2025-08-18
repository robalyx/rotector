package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/internal/wordlist"
	"github.com/tailscale/hujson"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// Setup dependencies
	deps, err := setupDependencies()
	if err != nil {
		return fmt.Errorf("failed to setup dependencies: %w", err)
	}

	app := &cli.Command{
		Name:  "wordlist",
		Usage: "Wordlist validation and analysis tool",
		Commands: []*cli.Command{
			{
				Name:  "check",
				Usage: "Check wordlist for errors",
				Description: `Check wordlist for actual errors:
- Exact duplicate terms
- Cross-reference duplicates  
- Self-references
- Substring redundancy (shorter terms contained in longer ones)
- Morphological redundancy (manual variants that checker handles automatically)
- Empty required fields
- Invalid severity/category values

Returns exit code 1 if errors found, 0 if clean.`,
				Action: func(_ context.Context, _ *cli.Command) error {
					issues := wordlist.ValidateWordlist(deps.wordlist)

					if len(issues) > 0 {
						fmt.Printf("❌ Found %d error(s):\n\n", len(issues))
						for _, issue := range issues {
							fmt.Printf("• %s\n", issue.Description)
						}
						return cli.Exit("", 1)
					}

					fmt.Println("✅ No errors found")
					return nil
				},
			},
		},
	}

	return app.Run(context.Background(), os.Args)
}

// cliDependencies holds the common dependencies needed by CLI commands.
type cliDependencies struct {
	wordlist *config.Wordlist
	logger   *zap.Logger
}

// setupDependencies initializes all dependencies needed by the CLI.
func setupDependencies() (*cliDependencies, error) {
	// Create development logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Load wordlist from hardcoded path (JSONC format)
	wordlistPath := "config/wordlist.jsonc"

	data, err := os.ReadFile(wordlistPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn("Wordlist file not found",
				zap.String("path", wordlistPath))

			return &cliDependencies{
				wordlist: &config.Wordlist{Terms: []config.WordlistEntry{}},
				logger:   logger,
			}, nil
		}

		return nil, fmt.Errorf("failed to read wordlist file: %w", err)
	}

	// Parse JSONC (strip comments and normalize to standard JSON)
	standardJSON, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("failed to standardize JSONC: %w", err)
	}

	// Parse wordlist
	var wordlist config.Wordlist
	if err := json.Unmarshal(standardJSON, &wordlist); err != nil {
		return nil, fmt.Errorf("failed to parse wordlist JSON: %w", err)
	}

	logger.Info("Loaded wordlist",
		zap.Int("terms", len(wordlist.Terms)),
		zap.String("path", wordlistPath))

	return &cliDependencies{
		wordlist: &wordlist,
		logger:   logger,
	}, nil
}
