package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"
	"sync"

	"github.com/HugoSmits86/nativewebp"
	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/jaxron/axonet/pkg/client"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// OutfitSystemPrompt provides detailed instructions to the AI model for analyzing user outfits.
	OutfitSystemPrompt = `You are a Roblox moderator analyzing outfits for predatory behavior targeting minors.

Each outfit image is provided as a separate image part along with this prompt.
The first image (if present) is the user's current outfit, followed by their outfit images.
You will receive a list of outfit names that correspond to the images in order.

Output format:
{
  "username": "string",
  "reason": "Clear explanation in one sentence",
  "evidence": ["exact outfit name 1", "exact outfit name 2"],
  "confidence": 0.0-1.0
}

Confidence levels:
0.0: No violations
0.1-0.3: One inappropriate outfit
0.4-0.6: Two inappropriate outfits
0.7-0.8: Three inappropriate outfits
0.9-1.0: Four+ inappropriate outfits

Key rules:
1. Return ONLY users with violations
2. Use "the outfit" instead of outfit names in reasons
3. Include exact outfit names in evidence ONLY if their images violate guidelines
4. Each evidence entry must be a single, complete outfit name - do not combine names
5. NEVER flag an outfit based on its name alone - analyze only the visual content
6. Ensure outfit names in evidence match exactly with the provided names
7. Do not modify, combine, or alter outfit names in any way

Flag outfits containing:
- Inappropriate or overly revealing swimwear
- Skintight or suggestive bodysuits designed to mimic nudity
- Oversized or emphasized chest/breast areas
- Exaggerated sexual characteristics
- Unrealistic body proportions meant to be suggestive
- Outfits with suggestive protrusions or attachments mimicking genitalia
- Bondage, BDSM, or fetish-themed clothing and accessories
- Explicit roleplay outfits (e.g., suggestive maid, nurse, etc.)
- Collar and leash combinations
- Outfits designed to simulate sexual acts or positions
- Clothing with explicit or suggestive text/imagery
- Outfits that deliberately expose or emphasize genital or chest areas
- Revealing "cow girl" or similar animal costumes
- Provocative furry or anthropomorphic designs

Do NOT Flag:
- Outfits based on their names alone
- Standard, appropriately designed swimwear or athletic wear
- Regular, non-revealing casual clothing or fashion items
- Default Roblox clothing and official outfits
- Non-suggestive costumes, uniforms, or roleplay outfits
- Standard or proportionate body types and avatars
- Non-human avatars, including animals, robots, or fantasy characters
- Sci-fi, medieval, or artistic designs that are non-explicit
- Single minor violations that do not suggest inappropriate intent
- Accessories that are clearly weapons, tools, or non-sexual objects`

	// OutfitRequestPrompt provides a reminder to follow system guidelines for outfit analysis.
	OutfitRequestPrompt = `Analyze these outfits for inappropriate content.

Remember:
1. Each image part corresponds to the outfit name at the same position in the list
2. The first image (if present) is always the current outfit
3. Use exact outfit names when providing evidence
4. Include only outfits whose IMAGES clearly violate the guidelines
5. DO NOT flag outfits based on their names - analyze only the visual content

Outfits to analyze (in order of corresponding images):
`
)

const (
	MaxOutfits = 9
)

var (
	ErrNoViolations        = errors.New("no violations found in outfits")
	ErrNoOutfits           = errors.New("no outfit images downloaded successfully")
	ErrInvalidThumbnailURL = errors.New("invalid thumbnail URL")
)

// OutfitAnalysis contains the AI's analysis results for a user's outfits.
type OutfitAnalysis struct {
	Username   string   `json:"username"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
	Confidence float64  `json:"confidence"`
}

// OutfitAnalyzer handles AI-based outfit analysis using Gemini models.
type OutfitAnalyzer struct {
	httpClient       *client.Client
	outfitModel      *genai.GenerativeModel
	thumbnailFetcher *fetcher.ThumbnailFetcher
	analysisSem      *semaphore.Weighted
	logger           *zap.Logger
}

// DownloadResult contains the result of a single outfit image download.
type DownloadResult struct {
	img  image.Image
	name string
}

// NewOutfitAnalyzer creates an OutfitAnalyzer instance.
func NewOutfitAnalyzer(app *setup.App, logger *zap.Logger) *OutfitAnalyzer {
	// Create outfit analysis model
	outfitModel := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	outfitModel.SystemInstruction = genai.NewUserContent(genai.Text(OutfitSystemPrompt))
	outfitModel.ResponseMIMEType = ApplicationJSON
	outfitModel.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"username": {
				Type:        genai.TypeString,
				Description: "Username of the account being analyzed",
			},
			"reason": {
				Type:        genai.TypeString,
				Description: "Clear explanation of violations found in outfits",
			},
			"evidence": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeString,
				},
				Description: "Names of outfits that have violations",
			},
			"confidence": {
				Type:        genai.TypeNumber,
				Description: "Confidence level based on severity of violations found",
			},
		},
		Required: []string{"username", "reason", "evidence", "confidence"},
	}
	outfitModel.Temperature = utils.Ptr(float32(0.2))
	outfitModel.TopP = utils.Ptr(float32(0.1))
	outfitModel.TopK = utils.Ptr(int32(1))

	return &OutfitAnalyzer{
		httpClient:       app.RoAPI.GetClient(),
		outfitModel:      outfitModel,
		thumbnailFetcher: fetcher.NewThumbnailFetcher(app.RoAPI, logger),
		analysisSem:      semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.OutfitAnalysis)),
		logger:           logger.Named("ai_outfit"),
	}
}

// ProcessOutfits analyzes outfit images for a batch of users.
func (a *OutfitAnalyzer) ProcessOutfits(userInfos []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType]) {
	// Filter userInfos to only include already flagged users
	var flaggedInfos []*types.User
	for _, info := range userInfos {
		if _, isFlagged := reasonsMap[info.ID]; isFlagged {
			flaggedInfos = append(flaggedInfos, info)
		}
	}

	// Skip if no flagged users to process
	if len(flaggedInfos) == 0 {
		return
	}

	// Get all outfit thumbnails organized by user
	userOutfits, userThumbnails := a.getOutfitThumbnails(context.Background(), flaggedInfos)

	// Process each user's outfits concurrently
	var (
		p  = pool.New().WithContext(context.Background())
		mu sync.Mutex
	)

	for _, userInfo := range flaggedInfos {
		// Skip if user has no outfits
		outfits, hasOutfits := userOutfits[userInfo.ID]
		if !hasOutfits {
			continue
		}

		thumbnails := userThumbnails[userInfo.ID]

		p.Go(func(ctx context.Context) error {
			// Analyze user's outfits
			err := a.analyzeUserOutfits(ctx, userInfo, &mu, reasonsMap, outfits, thumbnails)
			if err != nil && !errors.Is(err, ErrNoViolations) {
				a.logger.Error("Failed to analyze outfits",
					zap.Error(err),
					zap.Uint64("userID", userInfo.ID))
				return err
			}
			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Error during outfit analysis", zap.Error(err))
		return
	}

	a.logger.Info("Received AI outfit analysis for flagged users",
		zap.Int("totalUsers", len(flaggedInfos)))
}

// getOutfitThumbnails fetches thumbnail URLs for outfits and organizes them by user.
func (a *OutfitAnalyzer) getOutfitThumbnails(
	ctx context.Context, userInfos []*types.User,
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
				Format:    apiTypes.JPEG,
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

// analyzeUserOutfits handles the analysis of a single user's outfits.
func (a *OutfitAnalyzer) analyzeUserOutfits(
	ctx context.Context, info *types.User, mu *sync.Mutex, reasonsMap map[uint64]types.Reasons[enum.UserReasonType],
	outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string,
) error {
	// Acquire semaphore before making AI request
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Analyze outfits with retry
	analysis, err := utils.WithRetry(ctx, func() (*OutfitAnalysis, error) {
		// Create separate image parts from outfits
		var parts []genai.Part
		outfitNames := make([]string, 0, len(outfits))

		// Download and process each outfit image
		downloads, err := a.downloadOutfitImages(ctx, info, outfits, thumbnailMap)
		if err != nil {
			if errors.Is(err, ErrNoOutfits) {
				return nil, ErrNoViolations
			}
			return nil, fmt.Errorf("failed to download outfit images: %w", err)
		}

		// Process each downloaded image
		for _, result := range downloads {
			buf := new(bytes.Buffer)
			if err := nativewebp.Encode(buf, result.img, nil); err != nil {
				continue
			}
			parts = append(parts, genai.ImageData("webp", buf.Bytes()))
			outfitNames = append(outfitNames, result.name)
		}

		// Prepare prompt with outfit information
		prompt := fmt.Sprintf(
			"%s\n\nAnalyze outfits for user %q.\nOutfit names: %s",
			OutfitRequestPrompt,
			info.Name,
			strings.Join(outfitNames, ", "),
		)

		// Send request to Gemini with all image parts
		modelParts := append([]genai.Part{genai.Text(prompt)}, parts...)
		resp, err := a.outfitModel.GenerateContent(ctx, modelParts...)
		if err != nil {
			return nil, fmt.Errorf("gemini API error: %w", err)
		}

		// Check for empty response
		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
		}

		// Parse response from AI
		responseText, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
		if !ok {
			return nil, fmt.Errorf("%w: unexpected response format from AI", ErrModelResponse)
		}

		// Parse the JSON response
		var result OutfitAnalysis
		if err := sonic.Unmarshal([]byte(responseText), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return &result, nil
	}, utils.GetAIRetryOptions())
	if err != nil {
		return err
	}

	// Skip results with no violations
	if analysis.Confidence < 0.1 || analysis.Reason == "NO_VIOLATIONS" {
		return nil
	}

	// Validate confidence level
	if analysis.Confidence > 1.0 {
		a.logger.Debug("AI flagged user with invalid confidence",
			zap.String("username", info.Name),
			zap.Float64("confidence", analysis.Confidence))
		return nil
	}

	// If analysis is successful and violations found, update reasons map
	mu.Lock()
	if _, exists := reasonsMap[info.ID]; !exists {
		reasonsMap[info.ID] = make(types.Reasons[enum.UserReasonType])
	}
	reasonsMap[info.ID].Add(enum.UserReasonTypeOutfit, &types.Reason{
		Message:    analysis.Reason,
		Confidence: analysis.Confidence,
		Evidence:   analysis.Evidence,
	})
	mu.Unlock()

	a.logger.Info("AI flagged user with outfit violations",
		zap.Uint64("userID", info.ID),
		zap.String("username", info.Name),
		zap.String("reason", analysis.Reason),
		zap.Float64("confidence", analysis.Confidence))

	return nil
}

// downloadOutfitImages concurrently downloads outfit images until we have enough.
func (a *OutfitAnalyzer) downloadOutfitImages(
	ctx context.Context, userInfo *types.User, outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string,
) ([]DownloadResult, error) {
	var downloads []DownloadResult

	// Download current user thumbnail
	thumbnailURL := userInfo.ThumbnailURL
	if thumbnailURL != "" && thumbnailURL != fetcher.ThumbnailPlaceholder {
		if thumbnailImg, ok := a.downloadImage(ctx, thumbnailURL); ok {
			downloads = append(downloads, DownloadResult{
				img:  thumbnailImg,
				name: "Current Outfit",
			})
		}
	}

	// Process outfits concurrently
	var (
		p  = pool.New().WithContext(ctx)
		mu sync.Mutex
	)

	for _, outfit := range outfits {
		// Check if thumbnail is valid
		thumbnailURL, ok := thumbnailMap[outfit.ID]
		if !ok || thumbnailURL == "" || thumbnailURL == fetcher.ThumbnailPlaceholder {
			continue
		}

		p.Go(func(ctx context.Context) error {
			img, ok := a.downloadImage(ctx, thumbnailURL)
			if !ok {
				return nil
			}

			mu.Lock()
			downloads = append(downloads, DownloadResult{
				img:  img,
				name: outfit.Name,
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

	return downloads, nil
}

// downloadImage downloads an image from a URL.
func (a *OutfitAnalyzer) downloadImage(ctx context.Context, url string) (image.Image, bool) {
	// Download image
	resp, err := a.httpClient.NewRequest().URL(url).Do(ctx)
	if err != nil {
		a.logger.Warn("Failed to download outfit image",
			zap.Error(err),
			zap.String("url", url))
		return nil, false
	}
	defer resp.Body.Close()

	// Decode image
	img, err := nativewebp.Decode(resp.Body)
	if err != nil {
		return nil, false
	}

	return img, true
}
