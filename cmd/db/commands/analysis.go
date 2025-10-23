package commands

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// TermFrequency represents a term and its occurrence count.
type TermFrequency struct {
	Term  string
	Count int
}

// AnalysisCommands returns all analysis-related commands.
func AnalysisCommands(deps *CLIDependencies) []*cli.Command {
	return []*cli.Command{
		{
			Name:  "extract-terms",
			Usage: "Extract common terms from profile reason evidences and save to CSV",
			Description: `Extract and analyze common terms from flagged user profile evidences.
This command analyzes the evidence field of profile reasons to identify frequently
used inappropriate terms and outputs them to a CSV file with term and count columns.

Examples:
  db extract-terms --output terms.csv                 # Extract all terms to CSV
  db extract-terms --output terms.csv --min-count 5   # Only include terms appearing 5+ times
  db extract-terms --output terms.csv --max-terms 50  # Limit to top 50 most common terms
  db extract-terms --output terms.csv --confidence 0.8 # Only analyze high-confidence users`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "output",
					Usage:    "Output CSV file path (required)",
					Required: true,
				},
				&cli.IntFlag{
					Name:  "min-count",
					Usage: "Minimum number of occurrences for a term to be included",
					Value: 3,
				},
				&cli.IntFlag{
					Name:  "max-terms",
					Usage: "Maximum number of terms to include",
					Value: 100,
				},
				&cli.Float64Flag{
					Name:  "confidence",
					Usage: "Minimum confidence threshold for users to analyze",
					Value: 0,
				},
				&cli.IntFlag{
					Name:  "limit",
					Usage: "Limit the number of users to analyze (0 = no limit)",
					Value: 0,
				},
			},
			Action: handleExtractTerms(deps),
		},
	}
}

// handleExtractTerms handles the 'extract-terms' command.
func handleExtractTerms(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		// Get parameters from flags
		minCount := max(c.Int("min-count"), 1)
		maxTerms := max(c.Int("max-terms"), 10)
		confidenceThreshold := c.Float64("confidence")
		outputFile := c.String("output")
		limit := c.Int("limit")

		deps.Logger.Info("Starting term extraction",
			zap.Int("minCount", minCount),
			zap.Int("maxTerms", maxTerms),
			zap.Float64("confidenceThreshold", confidenceThreshold),
			zap.String("outputFile", outputFile),
			zap.Int("limit", limit))

		// Get flagged users with profile reasons
		users, err := deps.DB.Service().User().GetFlaggedUsersWithProfileReasons(ctx, confidenceThreshold, limit)
		if err != nil {
			return fmt.Errorf("failed to get flagged users: %w", err)
		}

		if len(users) == 0 {
			deps.Logger.Info("No flagged users with profile reasons found")
			return nil
		}

		deps.Logger.Info("Found flagged users", zap.Int("count", len(users)))

		// Extract terms from evidence
		termCounts := make(map[string]int)

		for _, user := range users {
			if user.Reasons == nil {
				continue
			}

			profileReason := user.Reasons[enum.UserReasonTypeProfile]
			if profileReason == nil || profileReason.Evidence == nil {
				continue
			}

			// Extract terms from evidence
			for _, evidence := range profileReason.Evidence {
				terms := extractTermsFromText(evidence)
				for _, term := range terms {
					termCounts[term]++
				}
			}
		}

		if len(termCounts) == 0 {
			deps.Logger.Info("No terms found in evidence")
			return nil
		}

		deps.Logger.Info("Extracted terms", zap.Int("uniqueTerms", len(termCounts)))

		// Filter by minimum count and sort by frequency
		var termFrequencies []TermFrequency

		for term, count := range termCounts {
			if count >= minCount {
				termFrequencies = append(termFrequencies, TermFrequency{
					Term:  term,
					Count: count,
				})
			}
		}

		// Sort by frequency (descending)
		sort.Slice(termFrequencies, func(i, j int) bool {
			return termFrequencies[i].Count > termFrequencies[j].Count
		})

		// Limit results
		if len(termFrequencies) > maxTerms {
			termFrequencies = termFrequencies[:maxTerms]
		}

		deps.Logger.Info("Filtered and sorted terms",
			zap.Int("count", len(termFrequencies)),
			zap.Int("minCount", minCount))

		// Write results to CSV file
		file, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		defer writer.Flush()

		// Write CSV header
		if err := writer.Write([]string{"term", "count"}); err != nil {
			return fmt.Errorf("failed to write CSV header: %w", err)
		}

		// Write term data
		for _, tf := range termFrequencies {
			record := []string{tf.Term, strconv.Itoa(tf.Count)}
			if err := writer.Write(record); err != nil {
				return fmt.Errorf("failed to write CSV record: %w", err)
			}
		}

		deps.Logger.Info("Saved terms to CSV file",
			zap.String("file", outputFile),
			zap.Int("totalUsers", len(users)),
			zap.Int("uniqueTerms", len(termCounts)),
			zap.Int("savedTerms", len(termFrequencies)))

		return nil
	}
}

// extractTermsFromText extracts potential inappropriate terms from evidence text.
func extractTermsFromText(text string) []string {
	var terms []string

	// Convert to lowercase for analysis
	text = strings.ToLower(text)

	// Define patterns to extract potential terms
	// Look for quoted terms, standalone words, and suspicious patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`"([^"]+)"`),                    // Quoted terms
		regexp.MustCompile(`'([^']+)'`),                    // Single quoted terms
		regexp.MustCompile(`\b([a-zA-Z]{3,15})\b`),         // Standalone words 3-15 chars
		regexp.MustCompile(`\b([a-zA-Z]+[0-9]+[a-zA-Z]*)`), // Alphanumeric combinations
		regexp.MustCompile(`\b([0-9]+[a-zA-Z]+[0-9]*)`),    // Number-letter combinations
	}

	// Extract terms using patterns
	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) > 1 {
				term := strings.TrimSpace(match[1])
				if isValidTerm(term) {
					terms = append(terms, term)
				}
			}
		}
	}

	// Also extract individual words that might be suspicious
	words := strings.FieldsSeq(text)
	for word := range words {
		// Clean word of punctuation
		cleaned := strings.FieldsFunc(word, func(c rune) bool {
			return !unicode.IsLetter(c) && !unicode.IsNumber(c)
		})

		for _, cleanWord := range cleaned {
			if isValidTerm(cleanWord) {
				terms = append(terms, cleanWord)
			}
		}
	}

	return removeDuplicates(terms)
}

// isValidTerm checks if a term should be considered for extraction.
func isValidTerm(term string) bool {
	// Filter out common words, very short terms, and very long terms
	if len(term) < 3 || len(term) > 20 {
		return false
	}

	// Filter out common English words that are unlikely to be inappropriate
	commonWords := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "are": {}, "but": {},
		"not": {}, "you": {}, "all": {}, "can": {}, "had": {},
		"her": {}, "was": {}, "one": {}, "our": {}, "out": {},
		"day": {}, "get": {}, "has": {}, "him": {}, "his": {},
		"how": {}, "man": {}, "new": {}, "now": {}, "old": {},
		"see": {}, "two": {}, "who": {}, "boy": {}, "did": {},
		"its": {}, "let": {}, "put": {}, "say": {}, "she": {},
		"too": {}, "use": {}, "way": {}, "will": {}, "with": {},
		"have": {}, "this": {}, "that": {}, "they": {}, "from": {},
		"said": {}, "what": {}, "were": {}, "been": {}, "good": {},
		"much": {}, "some": {}, "time": {}, "very": {}, "when": {},
		"come": {}, "here": {}, "just": {}, "like": {}, "long": {},
		"make": {}, "many": {}, "over": {}, "such": {}, "take": {},
		"than": {}, "them": {}, "well": {},
	}

	_, exists := commonWords[strings.ToLower(term)]

	return !exists
}

// removeDuplicates removes duplicate terms from a slice.
func removeDuplicates(terms []string) []string {
	keys := make(map[string]struct{})

	var result []string

	for _, term := range terms {
		if _, exists := keys[term]; !exists {
			keys[term] = struct{}{}
			result = append(result, term)
		}
	}

	return result
}
