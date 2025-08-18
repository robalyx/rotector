package checker

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/robalyx/rotector/internal/ai"
	dbtypes "github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// WordlistCheckerParams contains parameters for wordlist checking.
type WordlistCheckerParams struct {
	Users           []*dbtypes.ReviewUser
	TranslatedInfos map[string]*dbtypes.ReviewUser
}

// WordlistChecker handles checking user profiles against a wordlist.
type WordlistChecker struct {
	app              *setup.App
	wordlist         *config.Wordlist
	wordlistAnalyzer *ai.WordlistAnalyzer
	logger           *zap.Logger
	normalizer       *utils.TextNormalizer
	regexCache       map[string]*regexp.Regexp
	mu               sync.RWMutex
}

// NewWordlistChecker creates a new WordlistChecker.
func NewWordlistChecker(app *setup.App, wordlist *config.Wordlist, logger *zap.Logger) *WordlistChecker {
	return &WordlistChecker{
		app:              app,
		wordlist:         wordlist,
		wordlistAnalyzer: ai.NewWordlistAnalyzer(app, logger),
		logger:           logger.Named("wordlist_checker"),
		normalizer:       utils.NewTextNormalizer(),
		regexCache:       make(map[string]*regexp.Regexp),
	}
}

// ProcessUsers processes AI-rejected users through wordlist checking.
func (c *WordlistChecker) ProcessUsers(
	ctx context.Context, rejectedUsers []*dbtypes.ReviewUser, rejectedUserData map[int64]ai.RejectedUser,
	translatedInfos map[string]*dbtypes.ReviewUser,
) map[int64]ai.UserReasonRequest {
	acceptedUsers := make(map[int64]ai.UserReasonRequest)

	// Check each rejected user against wordlist
	wordlistFlagged := c.checkUsers(ctx, &WordlistCheckerParams{
		Users:           rejectedUsers,
		TranslatedInfos: translatedInfos,
	})

	// Process users that are flagged by wordlist analysis
	for _, user := range rejectedUsers {
		_, isFlagged := wordlistFlagged[user.ID]
		if !isFlagged {
			continue
		}

		// Get the original AI analysis data for this user
		rejectedUserInfo, exists := rejectedUserData[user.ID]
		if !exists {
			c.logger.Warn("Rejected user not found in rejected user data map",
				zap.Int64("userID", user.ID),
				zap.String("username", user.Name))

			continue
		}

		// Accept user with original AI analysis data
		acceptedUsers[user.ID] = rejectedUserInfo.UserReasonRequest

		c.logger.Debug("Accepted AI-rejected user via wordlist analysis",
			zap.Int64("userID", user.ID),
			zap.String("username", user.Name),
			zap.Float64("confidence", rejectedUserInfo.UserReasonRequest.Confidence))
	}

	c.logger.Info("Completed wordlist processing of AI-rejected users",
		zap.Int("rejectedUsers", len(rejectedUsers)),
		zap.Int("acceptedUsers", len(acceptedUsers)))

	return acceptedUsers
}

// checkUsers checks users against the wordlist and returns flagging decisions.
func (c *WordlistChecker) checkUsers(ctx context.Context, params *WordlistCheckerParams) map[int64]struct{} {
	c.mu.RLock()
	wordlist := c.wordlist
	c.mu.RUnlock()

	flaggedUsers := make(map[int64]struct{})

	if wordlist == nil || len(wordlist.Terms) == 0 {
		return flaggedUsers
	}

	// Check users against wordlist and build AI analysis requests
	analysisRequests := make([]ai.WordlistAnalysisRequest, 0, len(params.Users))
	userIDToName := make(map[string]int64)
	totalMatches := 0

	for _, user := range params.Users {
		// Use translated version if available
		targetUser := user
		if params.TranslatedInfos != nil {
			if translatedUser, exists := params.TranslatedInfos[user.Name]; exists {
				targetUser = translatedUser
			}
		}

		// Check user profile against wordlist
		userMatches := c.checkUserContent(targetUser, wordlist)

		// Skip user if no wordlist matches
		if len(userMatches) == 0 {
			continue
		}

		totalMatches++

		// Extract matched terms for logging
		matchedTerms := make([]string, len(userMatches))
		for i, match := range userMatches {
			matchedTerms[i] = match.MatchedTerm
		}

		c.logger.Info("Found initial wordlist matches for user",
			zap.Int64("userID", user.ID),
			zap.String("username", user.Name),
			zap.Int("matches", len(userMatches)),
			zap.Strings("matchedTerms", matchedTerms))

		// Build AI analysis request only for users with matches
		analysisRequests = append(analysisRequests, ai.WordlistAnalysisRequest{
			Name:              targetUser.Name,
			DisplayName:       targetUser.DisplayName,
			Description:       targetUser.Description,
			UserReasonRequest: nil, // Not available in this context
			FlaggedMatches:    userMatches,
		})
		userIDToName[targetUser.Name] = user.ID
	}

	// Analyze users using AI
	analysisResults := c.wordlistAnalyzer.AnalyzeUsers(ctx, analysisRequests, 0)

	// Convert analysis results back to user ID mapping
	for username := range analysisResults {
		if userID, exists := userIDToName[username]; exists {
			flaggedUsers[userID] = struct{}{}

			c.logger.Info("AI flagged user",
				zap.Int64("userID", userID),
				zap.String("username", username))
		}
	}

	c.logger.Info("Completed wordlist and content analysis",
		zap.Int("totalUsers", len(params.Users)),
		zap.Int("usersWithMatches", totalMatches),
		zap.Int("usersAnalyzedByAI", len(analysisRequests)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return flaggedUsers
}

// checkUserContent checks a user's content against the wordlist.
func (c *WordlistChecker) checkUserContent(
	user *dbtypes.ReviewUser, wordlist *config.Wordlist,
) []config.WordlistMatch {
	var matches []config.WordlistMatch

	for _, entry := range wordlist.Terms {
		// Generate all terms to check: primary term, related terms, and morphological variations
		termsToCheck := c.generateAllTermVariations(&entry)

		// Sort terms by length (shortest first as base forms are usually shorter)
		sort.Slice(termsToCheck, func(i, j int) bool {
			return len(termsToCheck[i]) < len(termsToCheck[j])
		})

		// Check username and display name
		if match := c.checkFieldForFirstMatch(user.Name, &entry, termsToCheck); match != nil {
			matches = append(matches, *match)
			continue
		}

		if user.DisplayName != "" && user.DisplayName != user.Name {
			if match := c.checkFieldForFirstMatch(user.DisplayName, &entry, termsToCheck); match != nil {
				matches = append(matches, *match)
				continue
			}
		}

		// Check description
		if !entry.NameOnly && user.Description != "" && user.Description != "No description" {
			if match := c.checkFieldForFirstMatch(user.Description, &entry, termsToCheck); match != nil {
				matches = append(matches, *match)
				continue
			}
		}
	}

	return matches
}

// containsTermWithVariations checks if text contains the term with word boundaries.
func (c *WordlistChecker) containsTermWithVariations(text, term string, allowSubstring bool) bool {
	normalizedText := c.applyCharacterSubstitutions(text)
	normalizedTerm := c.applyCharacterSubstitutions(term)

	if allowSubstring {
		return strings.Contains(strings.ToLower(normalizedText), strings.ToLower(normalizedTerm))
	}

	regex := c.getCompiledRegex(normalizedTerm)

	return regex.MatchString(normalizedText)
}

// getCompiledRegex gets or compiles a regex for the given term with caching.
func (c *WordlistChecker) getCompiledRegex(normalizedTerm string) *regexp.Regexp {
	c.mu.RLock()

	if cached, exists := c.regexCache[normalizedTerm]; exists {
		c.mu.RUnlock()
		return cached
	}

	c.mu.RUnlock()

	pattern := `(?:^|[^a-zA-Z0-9])` + regexp.QuoteMeta(normalizedTerm) + `(?:[^a-zA-Z0-9]|$)`
	regex := regexp.MustCompile("(?i)" + pattern)

	c.mu.Lock()
	c.regexCache[normalizedTerm] = regex
	c.mu.Unlock()

	return regex
}

// applyCharacterSubstitutions applies character substitutions for obfuscation detection.
func (c *WordlistChecker) applyCharacterSubstitutions(text string) string {
	// Clean up text using normalizer
	normalized := c.normalizer.Normalize(text)
	if normalized == "" {
		normalized = strings.ToLower(text)
	}

	// Apply leetspeak substitutions
	replacements := map[string]string{
		"@": "a",
		"3": "e",
		"0": "o",
		"1": "i",
	}

	for old, new := range replacements {
		normalized = strings.ReplaceAll(normalized, old, new)
	}

	return normalized
}

// generateAllTermVariations generates all variations for a wordlist entry including morphological forms.
func (c *WordlistChecker) generateAllTermVariations(entry *config.WordlistEntry) []string {
	var allTerms []string

	seen := make(map[string]struct{})

	// Add primary term and its morphological variations
	primaryVariations := utils.GenerateMorphologicalVariations(entry.Term)
	for _, variation := range primaryVariations {
		if _, exists := seen[variation]; !exists {
			allTerms = append(allTerms, variation)
			seen[variation] = struct{}{}
		}
	}

	// Add related terms and their morphological variations
	for _, relatedTerm := range entry.RelatedTerms {
		relatedVariations := utils.GenerateMorphologicalVariations(relatedTerm)
		for _, variation := range relatedVariations {
			if _, exists := seen[variation]; !exists {
				allTerms = append(allTerms, variation)
				seen[variation] = struct{}{}
			}
		}
	}

	return allTerms
}

// checkFieldForFirstMatch checks a text field against terms and returns the first match found.
func (c *WordlistChecker) checkFieldForFirstMatch(text string, entry *config.WordlistEntry, termsToCheck []string) *config.WordlistMatch {
	for _, termToCheck := range termsToCheck {
		normalizedTerm := strings.ToLower(termToCheck)
		if c.containsTermWithVariations(text, normalizedTerm, entry.AllowSubstring) {
			match := config.WordlistMatch{
				PrimaryTerm: entry.Term,
				MatchedTerm: termToCheck,
				Meaning:     entry.Meaning,
				Severity:    entry.Severity,
				Category:    entry.Category,
			}

			return &match
		}
	}

	return nil
}
