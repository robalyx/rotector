package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/HugoSmits86/nativewebp"
	"github.com/bytedance/sonic"
	"github.com/corona10/goimagehash"
	httpClient "github.com/jaxron/axonet/pkg/client"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	MaxOutfits = 100
)

var (
	ErrNoViolations        = errors.New("no violations found in outfits")
	ErrNoOutfits           = errors.New("no outfit images downloaded successfully")
	ErrInvalidThumbnailURL = errors.New("invalid thumbnail URL")
	ErrUnsupportedSchema   = errors.New("unsupported schema type")
)

// OutfitThemeAnalysis contains the AI's theme detection results for a user's outfits.
type OutfitThemeAnalysis struct {
	Username string        `json:"username" jsonschema_description:"Username of the account being analyzed"`
	Themes   []OutfitTheme `json:"themes"   jsonschema_description:"List of themes detected in the outfits"`
}

// OutfitTheme represents a detected theme for a single outfit.
type OutfitTheme struct {
	OutfitName string  `json:"outfitName" jsonschema_description:"Name of the outfit with a detected theme"`
	Theme      string  `json:"theme"      jsonschema_description:"Description of the specific theme detected"`
	Confidence float64 `json:"confidence" jsonschema_description:"Confidence score for this theme detection (0.0-1.0)"`
}

// OutfitAnalysisSchema is the JSON schema for the outfit theme analysis response.
var OutfitAnalysisSchema = utils.GenerateSchema[OutfitThemeAnalysis]()

// OutfitAnalyzer handles AI-based outfit analysis using OpenAI models.
type OutfitAnalyzer struct {
	httpClient           *httpClient.Client
	chat                 client.ChatCompletions
	thumbnailFetcher     *fetcher.ThumbnailFetcher
	outfitReasonAnalyzer *OutfitReasonAnalyzer
	analysisSem          *semaphore.Weighted
	logger               *zap.Logger
	imageLogger          *zap.Logger
	imageDir             string
	model                string
	batchSize            int
	similarityThreshold  int
}

// DownloadResult contains the result of a single outfit image download.
type DownloadResult struct {
	img             image.Image
	hash            *goimagehash.ImageHash
	name            string
	isCurrentOutfit bool
	similarOutfits  []string
}

// NewOutfitAnalyzer creates an OutfitAnalyzer instance.
func NewOutfitAnalyzer(app *setup.App, logger *zap.Logger) *OutfitAnalyzer {
	// Get image logger
	imageLogger, imageDir, err := app.LogManager.GetImageLogger("outfit_analyzer")
	if err != nil {
		logger.Error("Failed to create image logger", zap.Error(err))
		imageLogger = logger
	}

	return &OutfitAnalyzer{
		httpClient:           app.RoAPI.GetClient(),
		chat:                 app.AIClient.Chat(),
		thumbnailFetcher:     fetcher.NewThumbnailFetcher(app.RoAPI, logger),
		outfitReasonAnalyzer: NewOutfitReasonAnalyzer(app, logger),
		analysisSem:          semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.OutfitAnalysis)),
		logger:               logger.Named("ai_outfit"),
		imageLogger:          imageLogger,
		imageDir:             imageDir,
		model:                app.Config.Common.OpenAI.OutfitModel,
		batchSize:            app.Config.Worker.BatchSizes.OutfitAnalysisBatch,
		similarityThreshold:  app.Config.Worker.ThresholdLimits.ImageSimilarityThreshold,
	}
}

// ProcessOutfits analyzes outfit images for a batch of users.
// Returns a map of user IDs to their flagged outfit names.
func (a *OutfitAnalyzer) ProcessOutfits(
	ctx context.Context, userInfos []*types.ReviewUser,
	reasonsMap map[uint64]types.Reasons[enum.UserReasonType],
	inappropriateOutfitFlags map[uint64]bool,
) map[uint64]map[string]struct{} {
	// Filter users based on inappropriate outfit flags and existing reasons
	usersToProcess := a.filterUsersForOutfitProcessing(userInfos, reasonsMap, inappropriateOutfitFlags)

	// Skip if no users need outfit processing
	if len(usersToProcess) == 0 {
		a.logger.Info("No users to process outfits for")
		return nil
	}

	// Get all outfit thumbnails organized by user
	userOutfits, userThumbnails := a.getOutfitThumbnails(ctx, usersToProcess)

	// Process each user's outfits concurrently
	var (
		p              = pool.New().WithContext(ctx)
		mu             sync.Mutex
		flaggedOutfits = make(map[uint64]map[string]struct{})
	)

	for _, userInfo := range usersToProcess {
		// Skip if user has no outfits
		outfits, hasOutfits := userOutfits[userInfo.ID]
		if !hasOutfits || len(outfits) == 0 {
			continue
		}

		thumbnails := userThumbnails[userInfo.ID]

		p.Go(func(ctx context.Context) error {
			// Analyze user's outfits for themes
			outfitNames, err := a.analyzeUserOutfits(ctx, userInfo, &mu, reasonsMap, outfits, thumbnails)
			if err != nil && !errors.Is(err, ErrNoViolations) {
				a.logger.Error("Failed to analyze outfit themes",
					zap.Error(err),
					zap.Uint64("userID", userInfo.ID))

				return err
			}

			if len(outfitNames) > 0 {
				mu.Lock()

				flaggedOutfits[userInfo.ID] = outfitNames

				mu.Unlock()
			}

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Error during outfit theme analysis", zap.Error(err))
		return flaggedOutfits
	}

	a.logger.Info("Received AI outfit theme analysis",
		zap.Int("processedUsers", len(usersToProcess)),
		zap.Int("flaggedUsers", len(flaggedOutfits)))

	// Generate detailed outfit reasons for flagged users
	if len(flaggedOutfits) > 0 {
		a.outfitReasonAnalyzer.ProcessFlaggedUsers(ctx, userInfos, reasonsMap)
	}

	return flaggedOutfits
}

// filterUsersForOutfitProcessing determines which users should be processed through outfit analysis.
func (a *OutfitAnalyzer) filterUsersForOutfitProcessing(
	userInfos []*types.ReviewUser, reasonsMap map[uint64]types.Reasons[enum.UserReasonType], inappropriateOutfitFlags map[uint64]bool,
) []*types.ReviewUser {
	var usersToProcess []*types.ReviewUser

	for _, userInfo := range userInfos {
		var shouldProcess bool

		// Check if user has existing violations
		hasExistingViolations := func() bool {
			reasons, exists := reasonsMap[userInfo.ID]
			return exists && len(reasons) > 0
		}()

		// Use explicit filtering logic when outfit flags are provided
		if inappropriateOutfitFlags != nil {
			if flag, exists := inappropriateOutfitFlags[userInfo.ID]; exists && flag {
				// User is explicitly marked for outfit analysis
				shouldProcess = true
			} else if !exists {
				// User not in flags map, not processed
				shouldProcess = false
			} else {
				// User explicitly not marked for outfit analysis, but process if they have violations
				shouldProcess = hasExistingViolations
			}
		} else {
			// Fallback to legacy behavior: only process users with existing violations
			shouldProcess = hasExistingViolations
		}

		if shouldProcess {
			usersToProcess = append(usersToProcess, userInfo)
		}
	}

	return usersToProcess
}

// analyzeUserOutfits handles the theme analysis of a single user's outfits.
func (a *OutfitAnalyzer) analyzeUserOutfits(
	ctx context.Context, info *types.ReviewUser, mu *sync.Mutex, reasonsMap map[uint64]types.Reasons[enum.UserReasonType],
	outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string,
) (map[string]struct{}, error) {
	// Download all outfit images
	downloads, err := a.downloadOutfitImages(ctx, info, outfits, thumbnailMap)
	if err != nil {
		if errors.Is(err, ErrNoOutfits) {
			return nil, ErrNoViolations
		}

		return nil, fmt.Errorf("failed to download outfit images: %w", err)
	}

	// Process outfits in batches
	var (
		allSuspiciousThemes []string
		highestConfidence   float64
		uniqueFlaggedCount  int
	)

	flaggedOutfits := make(map[string]struct{})

	for i := 0; i < len(downloads); i += a.batchSize {
		end := min(i+a.batchSize, len(downloads))
		batch := downloads[i:end]

		// Analyze the current batch
		analysis, err := a.analyzeOutfitBatch(ctx, info, batch)
		if err != nil {
			if errors.Is(err, ErrNoOutfits) {
				continue
			}

			a.logger.Warn("Failed to analyze outfit batch",
				zap.Error(err),
				zap.Int("batchIndex", i),
				zap.Int("batchSize", a.batchSize))

			continue
		}

		// Skip if no analysis or themes
		if analysis == nil || len(analysis.Themes) == 0 {
			continue
		}

		// Process themes from this batch
		for _, theme := range analysis.Themes {
			// Skip themes with invalid confidence
			if theme.Confidence < 0.1 || theme.Confidence > 1.0 {
				continue
			}

			allSuspiciousThemes = append(allSuspiciousThemes,
				fmt.Sprintf("%s|%s|%.2f", theme.OutfitName, theme.Theme, theme.Confidence))

			flaggedOutfits[theme.OutfitName] = struct{}{}
			uniqueFlaggedCount++

			// Also flag similar outfits that were deduplicated
			for _, download := range batch {
				if download.name == theme.OutfitName && len(download.similarOutfits) > 0 {
					for _, similarOutfit := range download.similarOutfits {
						similarConfidence := theme.Confidence * 0.9 // Reduce confidence by 10% for similar outfits
						allSuspiciousThemes = append(allSuspiciousThemes,
							fmt.Sprintf("%s|%s (similar to %s)|%.2f", similarOutfit, theme.Theme, theme.OutfitName, similarConfidence))

						flaggedOutfits[similarOutfit] = struct{}{}

						a.logger.Debug("Flagged similar outfit",
							zap.String("originalOutfit", theme.OutfitName),
							zap.String("similarOutfit", similarOutfit),
							zap.String("theme", theme.Theme),
							zap.Float64("originalConfidence", theme.Confidence),
							zap.Float64("similarConfidence", similarConfidence))
					}

					break
				}
			}

			// Track highest confidence
			if theme.Confidence > highestConfidence {
				highestConfidence = theme.Confidence
			}
		}
	}

	// Determine flagging criteria based on number of suspicious outfits
	shouldFlag := false
	finalConfidence := highestConfidence

	switch {
	case uniqueFlaggedCount > 1 && highestConfidence >= 0.5:
		shouldFlag = true
	case uniqueFlaggedCount == 1 && highestConfidence >= 0.7:
		shouldFlag = true
		finalConfidence = highestConfidence * 0.8 // Reduce confidence by 20% for single outfit cases
	default:
		a.logger.Info("AI did not flag user with outfit themes",
			zap.Uint64("userID", info.ID),
			zap.String("username", info.Name),
			zap.Float64("highestConfidence", highestConfidence),
			zap.Int("uniqueFlaggedCount", uniqueFlaggedCount),
			zap.Int("totalFlaggedOutfits", len(flaggedOutfits)),
			zap.Int("totalOutfits", len(downloads)),
		)
	}

	if shouldFlag {
		mu.Lock()

		if _, exists := reasonsMap[info.ID]; !exists {
			reasonsMap[info.ID] = make(types.Reasons[enum.UserReasonType])
		}

		reasonsMap[info.ID].Add(enum.UserReasonTypeOutfit, &types.Reason{
			Message:    "User has outfits with inappropriate themes.",
			Confidence: finalConfidence,
			Evidence:   allSuspiciousThemes,
		})
		mu.Unlock()

		a.logger.Info("AI flagged user with outfit themes",
			zap.Uint64("userID", info.ID),
			zap.String("username", info.Name),
			zap.Float64("finalConfidence", finalConfidence),
			zap.Int("uniqueFlaggedOutfits", uniqueFlaggedCount),
			zap.Int("totalFlaggedOutfits", len(flaggedOutfits)),
			zap.Int("numThemes", len(allSuspiciousThemes)),
			zap.Strings("themes", allSuspiciousThemes))
	}

	return flaggedOutfits, nil
}

// processOutfitBatch handles the AI analysis for a batch of outfit images.
func (a *OutfitAnalyzer) processOutfitBatch(
	ctx context.Context, info *types.ReviewUser, batch []DownloadResult,
) (*OutfitThemeAnalysis, error) {
	// Process each downloaded image and add as user message parts
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(OutfitSystemPrompt),
	}

	outfitNames := make([]string, 0, len(batch))
	validOutfits := make(map[string]struct{})

	for _, result := range batch {
		// Convert image to base64
		buf := new(bytes.Buffer)
		if err := nativewebp.Encode(buf, result.img, nil); err != nil {
			continue
		}

		base64Image := base64.StdEncoding.EncodeToString(buf.Bytes())

		// Add image as a user message
		imagePart := openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL: "data:image/webp;base64," + base64Image,
		})
		messages = append(messages, openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{imagePart}))

		// Store outfit name
		outfitNames = append(outfitNames, result.name)
		validOutfits[result.name] = struct{}{}
	}

	// Skip if no images were processed successfully
	if len(outfitNames) == 0 {
		return nil, ErrNoOutfits
	}

	// Add final user message with numbered outfit names
	outfitList := make([]string, 0, len(outfitNames))
	for i, name := range outfitNames {
		outfitList = append(outfitList, fmt.Sprintf("Image %d: %s", i+1, name))
	}

	prompt := fmt.Sprintf(
		"%s\n\nIdentify themes for user %q.\n\nOutfit mapping:\n%s\n\nAnalyze each image in order and use the EXACT outfit names listed above.",
		OutfitRequestPrompt,
		info.Name,
		strings.Join(outfitList, "\n"),
	)
	messages = append(messages, openai.UserMessage(prompt))

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: messages,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "outfitAnalysis",
					Description: openai.String("Analysis of user outfits"),
					Schema:      OutfitAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.model,
		Temperature: openai.Float(0.0),
		TopP:        openai.Float(0.1),
	}

	// Make API request
	var analysis OutfitThemeAnalysis

	err := a.chat.NewWithRetry(ctx, params, func(resp *openai.ChatCompletion, err error) error {
		// Handle API error
		if err != nil {
			return fmt.Errorf("openai API error: %w", err)
		}

		// Check for empty response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
			return fmt.Errorf("%w: no response from model", utils.ErrModelResponse)
		}

		// Extract thought process
		message := resp.Choices[0].Message
		if thought := message.JSON.ExtraFields["reasoning"]; thought.Valid() {
			a.logger.Debug("AI outfit analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response
		if err := sonic.Unmarshal([]byte(message.Content), &analysis); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		// Validate outfit names and filter out invalid ones
		var validThemes []OutfitTheme

		for _, theme := range analysis.Themes {
			if _, ok := validOutfits[theme.OutfitName]; ok {
				validThemes = append(validThemes, theme)
				continue
			}

			a.logger.Info("AI flagged non-existent outfit",
				zap.String("username", info.Name),
				zap.String("outfitName", theme.OutfitName))
		}

		analysis.Themes = validThemes

		return nil
	})

	return &analysis, err
}

// analyzeOutfitBatch processes a single batch of outfit images.
func (a *OutfitAnalyzer) analyzeOutfitBatch(
	ctx context.Context, info *types.ReviewUser, downloads []DownloadResult,
) (*OutfitThemeAnalysis, error) {
	// Acquire semaphore
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Handle content blocking
	minBatchSize := max(len(downloads)/4, 1)

	var result *OutfitThemeAnalysis

	err := utils.WithRetrySplitBatch(
		ctx, downloads, len(downloads), minBatchSize, utils.GetAIRetryOptions(),
		func(batch []DownloadResult) error {
			var err error

			result, err = a.processOutfitBatch(ctx, info, batch)

			return err
		},
		func(items []DownloadResult) {
			for i, item := range items {
				// Generate unique filename using outfit name
				filename := fmt.Sprintf("%d_%s.webp", i+1, strings.ReplaceAll(item.name, " ", "_"))
				filepath := filepath.Join(a.imageDir, filename)

				// Save image
				buf := new(bytes.Buffer)
				if err := nativewebp.Encode(buf, item.img, nil); err != nil {
					a.imageLogger.Error("Failed to encode blocked image",
						zap.Error(err),
						zap.String("outfitName", item.name))

					continue
				}

				if err := os.WriteFile(filepath, buf.Bytes(), 0o600); err != nil {
					a.imageLogger.Error("Failed to save blocked image",
						zap.Error(err),
						zap.String("outfitName", item.name),
						zap.String("path", filepath))

					continue
				}

				a.imageLogger.Info("Saved blocked image",
					zap.String("outfitName", item.name),
					zap.String("path", filepath))
			}
		},
	)

	return result, err
}

// getOutfitThumbnails fetches thumbnail URLs for outfits and organizes them by user.
func (a *OutfitAnalyzer) getOutfitThumbnails(
	ctx context.Context, userInfos []*types.ReviewUser,
) (map[uint64][]*apiTypes.Outfit, map[uint64]map[uint64]string) {
	userOutfits := make(map[uint64][]*apiTypes.Outfit)
	requests := thumbnails.NewBatchThumbnailsBuilder()

	// Organize outfits by user and build thumbnail requests
	for _, userInfo := range userInfos {
		// Limit outfits per user
		outfits := userInfo.Outfits
		if len(outfits) > MaxOutfits {
			outfits = outfits[:MaxOutfits]
		}

		userOutfits[userInfo.ID] = outfits

		// Add thumbnail requests for each outfit
		for _, outfit := range outfits {
			requests.AddRequest(apiTypes.ThumbnailRequest{
				Type:      apiTypes.OutfitType,
				TargetID:  outfit.ID,
				RequestID: strconv.FormatUint(outfit.ID, 10),
				Size:      apiTypes.Size150x150,
				Format:    apiTypes.WEBP,
			})
		}
	}

	// Get thumbnails for all outfits
	thumbnailMap := a.thumbnailFetcher.ProcessBatchThumbnails(ctx, requests)

	// Create user thumbnail map
	userThumbnails := make(map[uint64]map[uint64]string)
	for userID, outfits := range userOutfits {
		thumbnails := make(map[uint64]string)

		for _, outfit := range outfits {
			if url, ok := thumbnailMap[outfit.ID]; ok {
				thumbnails[outfit.ID] = url
			}
		}

		userThumbnails[userID] = thumbnails
	}

	return userOutfits, userThumbnails
}

// downloadOutfitImages concurrently downloads outfit images until we have enough.
func (a *OutfitAnalyzer) downloadOutfitImages(
	ctx context.Context, userInfo *types.ReviewUser, outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string,
) ([]DownloadResult, error) {
	var (
		p         = pool.New().WithContext(ctx)
		mu        sync.Mutex
		downloads []DownloadResult
	)

	// Download current user thumbnail
	thumbnailURL := userInfo.ThumbnailURL
	if thumbnailURL != "" && thumbnailURL != fetcher.ThumbnailPlaceholder {
		p.Go(func(ctx context.Context) error {
			img, hash, ok := a.downloadImage(ctx, thumbnailURL)
			if ok {
				mu.Lock()
				// Add current outfit at the beginning of the array
				downloads = append(downloads, DownloadResult{
					img:             img,
					hash:            hash,
					name:            "Current Outfit",
					isCurrentOutfit: true,
				})

				mu.Unlock()
			}

			return nil
		})
	}

	// Process outfits concurrently
	for _, outfit := range outfits {
		// Check if thumbnail is valid
		thumbnailURL, ok := thumbnailMap[outfit.ID]
		if !ok || thumbnailURL == "" || thumbnailURL == fetcher.ThumbnailPlaceholder {
			continue
		}

		p.Go(func(ctx context.Context) error {
			img, hash, ok := a.downloadImage(ctx, thumbnailURL)
			if !ok {
				return nil
			}

			mu.Lock()

			downloads = append(downloads, DownloadResult{
				img:             img,
				hash:            hash,
				name:            outfit.Name,
				isCurrentOutfit: false,
			})

			mu.Unlock()

			return nil
		})
	}

	// Wait for all downloads to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Error during outfit downloads", zap.Error(err))
	}

	// Check if we got any successful downloads
	if len(downloads) == 0 {
		return nil, ErrNoOutfits
	}

	// Deduplicate similar images
	deduplicatedDownloads := a.deduplicateImages(downloads)
	if len(deduplicatedDownloads) == 0 {
		return nil, ErrNoOutfits
	}

	return deduplicatedDownloads, nil
}

// downloadImage downloads an image from a URL and computes its perceptual hash.
func (a *OutfitAnalyzer) downloadImage(ctx context.Context, url string) (image.Image, *goimagehash.ImageHash, bool) {
	// Download image
	resp, err := a.httpClient.NewRequest().URL(url).Do(ctx)
	if err != nil {
		a.logger.Warn("Failed to download outfit image",
			zap.Error(err),
			zap.String("url", url))

		return nil, nil, false
	}
	defer resp.Body.Close()

	// Decode image
	img, err := nativewebp.Decode(resp.Body)
	if err != nil {
		return nil, nil, false
	}

	// Compute perceptual hash for deduplication
	hash, err := goimagehash.PerceptionHash(img)
	if err != nil {
		a.logger.Warn("Failed to compute perceptual hash",
			zap.Error(err),
			zap.String("url", url))
		// Still return the image even if hash computation fails
		return img, nil, true
	}

	return img, hash, true
}

// deduplicateImages removes similar images based on perceptual hashing.
// Returns a deduplicated slice of DownloadResult with unique images.
func (a *OutfitAnalyzer) deduplicateImages(downloads []DownloadResult) []DownloadResult {
	if len(downloads) <= 1 {
		return downloads
	}

	var deduplicated []DownloadResult

	for _, download := range downloads {
		// Always preserve the current outfit regardless of similarity
		if download.isCurrentOutfit {
			deduplicated = append(deduplicated, download)
			continue
		}

		// Skip if hash computation failed
		if download.hash == nil {
			continue
		}

		// Check if this image is similar to any previously processed image
		matchedIndex := -1

		for i, existing := range deduplicated {
			// Skip current outfit when checking similarity
			if existing.isCurrentOutfit || existing.hash == nil {
				continue
			}

			distance, err := download.hash.Distance(existing.hash)
			if err != nil {
				a.logger.Warn("Failed to compute hash distance",
					zap.Error(err),
					zap.String("outfitName", download.name))

				continue
			}

			// If images are similar, track this outfit as similar to the existing one
			if distance <= a.similarityThreshold {
				matchedIndex = i

				a.logger.Debug("Found similar outfit image",
					zap.String("outfitName", download.name),
					zap.String("similarTo", existing.name),
					zap.Int("distance", distance))

				break
			}
		}

		// If similar to an existing image, add to its similar outfits list
		if matchedIndex >= 0 {
			if deduplicated[matchedIndex].similarOutfits == nil {
				deduplicated[matchedIndex].similarOutfits = make([]string, 0)
			}

			deduplicated[matchedIndex].similarOutfits = append(deduplicated[matchedIndex].similarOutfits, download.name)
		} else {
			// If not similar to any existing image, add it to the deduplicated list
			deduplicated = append(deduplicated, download)
		}
	}

	return deduplicated
}
