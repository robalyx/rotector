package config

import (
	"fmt"
	"os"

	"github.com/bytedance/sonic"
	"github.com/tailscale/hujson"
)

// WordlistEntry represents a single term in the wordlist with its metadata.
type WordlistEntry struct {
	Term           string   `json:"term"`                     // Primary term
	RelatedTerms   []string `json:"relatedTerms"`             // Related terms, variations, abbreviations
	Meaning        string   `json:"meaning"`                  // Explanation of what this term means in inappropriate context
	Severity       string   `json:"severity"`                 // critical, high, medium, low
	Category       string   `json:"category"`                 // inappropriate_content, social_engineering, technical_evasion
	AllowSubstring bool     `json:"allowSubstring,omitempty"` // Allow matching as substring within words (not just word boundaries)
	NameOnly       bool     `json:"nameOnly,omitempty"`       // Only check in usernames/display names, not descriptions/bios
}

// Wordlist represents the full wordlist configuration.
type Wordlist struct {
	Terms []WordlistEntry `json:"terms"`
}

// WordlistMatch represents a match found in user content from wordlist checking.
type WordlistMatch struct {
	PrimaryTerm string // Primary term from wordlist entry
	MatchedTerm string // Actual term that matched (could be from relatedTerms)
	Meaning     string // Explanation of what this term means
	Severity    string // critical, high, medium, low
	Category    string // inappropriate_content, social_engineering, technical_evasion
}

// LoadWordlist loads the wordlist configuration from the first available path.
// It searches the same config paths as LoadConfig for consistency.
func LoadWordlist(configPath string) (*Wordlist, error) {
	// Try the specific config path
	if configPath != "" {
		if wordlist, err := loadWordlistFromPath(configPath + "/wordlist.jsonc"); err == nil {
			return wordlist, nil
		}
	}

	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// List search paths
	configPaths := []string{
		".rotector",
		homeDir + "/.rotector/config",
		"/etc/rotector/config",
		"/app/config",
		"config",
		".",
	}

	// Try to load wordlist from each path
	for _, path := range configPaths {
		wordlistPath := path + "/wordlist.jsonc"
		if wordlist, err := loadWordlistFromPath(wordlistPath); err == nil {
			return wordlist, nil
		}
	}

	return nil, ErrWordlistNotFound
}

// loadWordlistFromPath loads the wordlist from a specific file path.
func loadWordlistFromPath(wordlistPath string) (*Wordlist, error) {
	// Read wordlist file
	data, err := os.ReadFile(wordlistPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read wordlist file: %w", err)
	}

	// Parse JSONC
	standardJSON, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("failed to standardize JSONC: %w", err)
	}

	// Parse wordlist
	var wordlist Wordlist
	if err := sonic.Unmarshal(standardJSON, &wordlist); err != nil {
		return nil, fmt.Errorf("failed to parse wordlist JSON: %w", err)
	}

	return &wordlist, nil
}
