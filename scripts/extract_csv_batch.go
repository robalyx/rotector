package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/tailscale/hujson"
)

var errInsufficientData = errors.New("CSV file must have at least header + 1 data row")

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load existing wordlist to filter out already-included terms
	wordlistTerms, err := loadWordlistTerms()
	if err != nil {
		return fmt.Errorf("failed to load wordlist: %w", err)
	}

	// Read CSV file
	csvPath := "words.csv"

	file, err := os.Open(csvPath)
	if err != nil {
		return fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	// Read all lines
	var lines []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read CSV file: %w", err)
	}

	if len(lines) < 2 {
		return errInsufficientData
	}

	// Keep header, extract first 25 data lines (excluding already existing terms)
	header := lines[0]

	var (
		extractedLines []string
		remainingLines []string
	)

	extractedCount := 0

	for i := 1; i < len(lines); i++ {
		parts := strings.Split(lines[i], ",")
		if len(parts) < 1 {
			remainingLines = append(remainingLines, lines[i])
			continue
		}

		term := strings.TrimSpace(parts[0])
		if term == "" {
			remainingLines = append(remainingLines, lines[i])
			continue
		}

		// Check if term already exists in wordlist (including morphological variations)
		termVariations := utils.GenerateMorphologicalVariations(term)
		termExists := false

		for _, variation := range termVariations {
			if _, exists := wordlistTerms[strings.ToLower(variation)]; exists {
				termExists = true
				break
			}
		}

		if termExists {
			// Skip this term entirely - don't add to remaining
			continue
		}

		// Extract this term if we haven't reached the limit
		if extractedCount < 25 {
			extractedLines = append(extractedLines, lines[i])
			extractedCount++
		} else {
			// Add to remaining if we've already extracted 25
			remainingLines = append(remainingLines, lines[i])
		}
	}

	// Output extracted batch to console
	fmt.Println("=== EXTRACTED BATCH FOR ANALYSIS ===")
	fmt.Println(header)

	for _, line := range extractedLines {
		fmt.Println(line)
	}

	fmt.Println("=== END BATCH ===")

	// Update original CSV file with remaining lines
	updatedFile, err := os.Create(csvPath)
	if err != nil {
		return fmt.Errorf("failed to update CSV file: %w", err)
	}
	defer updatedFile.Close()

	fmt.Fprintln(updatedFile, header)

	for _, line := range remainingLines {
		fmt.Fprintln(updatedFile, line)
	}

	fmt.Printf("\nExtracted %d terms for analysis\n", len(extractedLines))
	fmt.Printf("Remaining %d terms in %s\n", len(remainingLines), csvPath)

	return nil
}

func loadWordlistTerms() (map[string]struct{}, error) {
	wordlistPath := "config/wordlist.jsonc"

	data, err := os.ReadFile(wordlistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]struct{}), nil
		}

		return nil, fmt.Errorf("failed to read wordlist file: %w", err)
	}

	// Parse JSONC
	standardJSON, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("failed to standardize JSONC: %w", err)
	}

	var wordlist config.Wordlist
	if err := json.Unmarshal(standardJSON, &wordlist); err != nil {
		return nil, fmt.Errorf("failed to parse wordlist JSON: %w", err)
	}

	// Build map of existing terms (primary + related + morphological variations)
	terms := make(map[string]struct{})

	for _, entry := range wordlist.Terms {
		// Add primary term and its morphological variations
		primaryVariations := utils.GenerateMorphologicalVariations(entry.Term)
		for _, variation := range primaryVariations {
			terms[strings.ToLower(variation)] = struct{}{}
		}

		// Add related terms and their morphological variations
		for _, related := range entry.RelatedTerms {
			relatedVariations := utils.GenerateMorphologicalVariations(related)
			for _, variation := range relatedVariations {
				terms[strings.ToLower(variation)] = struct{}{}
			}
		}
	}

	return terms, nil
}
