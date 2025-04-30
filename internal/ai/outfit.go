package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HugoSmits86/nativewebp"
	"github.com/bytedance/sonic"
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
	// OutfitSystemPrompt provides instructions for analyzing outfit themes.
	OutfitSystemPrompt = `Instruction:
You are a Roblox outfit analyzer detecting specific inappropriate outfit themes.
Each outfit image is provided as a separate image part along with this prompt.
The first image (if present) is the user's current outfit, followed by their outfit images.
You will receive a list of outfit names that correspond to the images in order.

Output format:
{
  "username": "string",
  "themes": [
    {
      "outfitName": "exact outfit name",
      "theme": "specific theme category: [detail]",
      "confidence": 0.0-1.0
    }
  ]
}

Theme categories (use this format):
- "Sexual/Adult: [specific detail]" (e.g., "Sexual/Adult: Revealing swimsuit with exaggerated anatomy")
- "Body/Figure: [specific detail]" (e.g., "Body/Figure: Exaggerated curvy avatar")
- "BDSM/Kink: [specific detail]" (e.g., "BDSM/Kink: Latex catsuit with chains")

Theme confidence levels based on severity:
0.0-0.3: Subtle or ambiguous theme elements
0.4-0.6: Clear but moderate theme elements
0.7-0.8: Strong and obvious theme elements
0.9-1.0: Extreme or explicit theme elements

Key instructions:
1. Return ONLY users with inappropriate themes
2. Include the exact outfit name
3. Only identify themes if they are clearly inappropriate in the image
4. Do not flag legitimate costume themes - focus only on inappropriate themes
5. Return empty themes array if no inappropriate themes are detected
6. Each theme detection should include the full outfit name, identified theme, and confidence level

Instruction: Pay close attention to outfits that are sexual or adult-themed:
- Stripper/pole dancer outfits
- Lingerie/underwear models
- Sexualized maid outfits (with fishnets, cleavage or inappropriate elements)
- Bunny girl outfits (lingerie versions)
- Latex catsuits/dominatrix outfits
- Fetishwear (bondage elements, suggestive accessories)
- Censored nudity looks (with pixelation, censor bars, stickers)
- Nudity with realistic body features (detailed abs, muscle definition, body hair, tattoos, etc.)
- Extremely revealing swimsuits/microkinis (especially with exaggerated anatomy)
- Provocative bodysuits with inappropriate cutouts
- Thongs/g-strings or outfits emphasizing exposed buttocks
- Outfits with intentional cleavage cutouts or revealing holes (heart-shaped, keyholes)
- Succubus-themed outfits (especially with womb tattoos or markings)

Instruction: Pay close attention to outfits that are body/figure-focused:
- Extremely exaggerated curvy/thicc avatars
- Unrealistic hourglass figures
- Inflated or exaggerated anatomy
- Ultra-slim waist avatars
- Bodies with sexualized scars or markings

Instruction: Pay close attention to outfits that are BDSM/kink/fetish parodies:
- Bondage sets (chains, gags, collars)
- Petplay (ears, collars, leashes in suggestive context)
- Slave-themed outfits (with chains, torn clothing)
- Leather harnesses/latex corsets in fetish context
- "Cow girl" outfits (NOT cowboy/western, but fetish-themed outfits with cow print, udders, or animal-sexualization elements)
- Suggestive schoolgirl outfits
- Diaper or "little" cosplays with sexualized elements

DO NOT flag these legitimate themes:
- Fantasy/mythology characters (e.g., gods, goddesses, mythical creatures)
- Monster/creature costumes (e.g., vampires, werewolves, zombies)
- Superhero/villain costumes
- Historical or cultural outfits
- Sci-fi or futuristic themes
- Animal or creature costumes (unless explicitly fetishized)
- Military or combat themes
- Professional or occupation-based outfits (unless sexualized)
- Cartoon or anime character costumes (unless explicitly inappropriate)
- Horror or spooky themes
- Modern streetwear or fashion trends
- Y2K or retro fashion styles
- Aesthetic-based outfits (cottagecore, dark academia, etc.)
- Beach or summer themed outfits of appropriate coverage
- Dance or performance outfits (unless explicitly inappropriate)
- Meme character outfits

DO NOT flag these legitimate modern fashion elements:
- Crop tops, midriff-showing tops
- Off-shoulder or cold-shoulder tops
- Ripped jeans or distressed clothing
- High-waisted or low-rise pants
- Bodycon dresses (unless extremely exaggerated)
- Athleisure or workout wear
- Shorts or skirts of reasonable length
- Swimwear of reasonable coverage
- Trendy cutouts in appropriate places
- Platform or high-heeled shoes
- Collar necklaces as fashion accessories
- Gothic or alternative fashion styles
- Punk or edgy fashion elements
- Cosplay or costume elements (unless explicitly inappropriate)`

	// OutfitRequestPrompt provides a reminder to focus on theme identification.
	OutfitRequestPrompt = `Identify specific themes in these outfits.

Remember:
1. Each image part corresponds to the outfit name at the same position in the list
2. The first image (if present) is always the current outfit
3. Only include outfits that clearly match one of the inappropriate themes
4. Return the exact outfit name in your analysis

Input:
`
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
	httpClient       *httpClient.Client
	chat             client.ChatCompletions
	thumbnailFetcher *fetcher.ThumbnailFetcher
	analysisSem      *semaphore.Weighted
	logger           *zap.Logger
	model            string
	batchSize        int
}

// DownloadResult contains the result of a single outfit image download.
type DownloadResult struct {
	img  image.Image
	name string
}

// NewOutfitAnalyzer creates an OutfitAnalyzer instance.
func NewOutfitAnalyzer(app *setup.App, logger *zap.Logger) *OutfitAnalyzer {
	return &OutfitAnalyzer{
		httpClient:       app.RoAPI.GetClient(),
		chat:             app.AIClient.Chat(),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(app.RoAPI, logger),
		analysisSem:      semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.OutfitAnalysis)),
		logger:           logger.Named("ai_outfit"),
		model:            app.Config.Common.OpenAI.OutfitModel,
		batchSize:        app.Config.Worker.BatchSizes.OutfitAnalysisBatch,
	}
}

// ProcessOutfits analyzes outfit images for a batch of users.
func (a *OutfitAnalyzer) ProcessOutfits(userInfos []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType]) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Get all outfit thumbnails organized by user
	userOutfits, userThumbnails := a.getOutfitThumbnails(ctx, userInfos)

	// Process each user's outfits concurrently
	var (
		p  = pool.New().WithContext(ctx)
		mu sync.Mutex
	)

	for _, userInfo := range userInfos {
		// Skip if user has no outfits
		outfits, hasOutfits := userOutfits[userInfo.ID]
		if !hasOutfits || len(outfits) == 0 {
			continue
		}

		thumbnails := userThumbnails[userInfo.ID]

		p.Go(func(ctx context.Context) error {
			// Analyze user's outfits for themes
			err := a.analyzeUserOutfits(ctx, userInfo, &mu, reasonsMap, outfits, thumbnails)
			if err != nil && !errors.Is(err, ErrNoViolations) {
				a.logger.Error("Failed to analyze outfit themes",
					zap.Error(err),
					zap.Uint64("userID", userInfo.ID))
				return err
			}
			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Error during outfit theme analysis", zap.Error(err))
		return
	}

	a.logger.Info("Received AI outfit theme analysis",
		zap.Int("totalUsers", len(userInfos)))
}

// analyzeUserOutfits handles the theme analysis of a single user's outfits.
func (a *OutfitAnalyzer) analyzeUserOutfits(
	ctx context.Context, info *types.User, mu *sync.Mutex, reasonsMap map[uint64]types.Reasons[enum.UserReasonType],
	outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string,
) error {
	// Download all outfit images
	downloads, err := a.downloadOutfitImages(ctx, info, outfits, thumbnailMap)
	if err != nil {
		if errors.Is(err, ErrNoOutfits) {
			return ErrNoViolations
		}
		return fmt.Errorf("failed to download outfit images: %w", err)
	}

	// Process outfits in batches
	var allSuspiciousThemes []string
	var highestConfidence float64

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

		// Process themes from this batch
		for _, theme := range analysis.Themes {
			// Skip themes with invalid confidence
			if theme.Confidence < 0.1 || theme.Confidence > 1.0 {
				continue
			}

			allSuspiciousThemes = append(allSuspiciousThemes,
				fmt.Sprintf("outfit|%s|theme|%s|confidence|%.2f", theme.OutfitName, theme.Theme, theme.Confidence))

			// Track highest confidence
			if theme.Confidence > highestConfidence {
				highestConfidence = theme.Confidence
			}
		}
	}

	// If no suspicious themes found, return
	if len(allSuspiciousThemes) == 0 {
		return nil
	}

	// Only flag if there are more than 1 suspicious outfits and confidence is high enough
	if len(allSuspiciousThemes) > 1 && highestConfidence >= 0.5 {
		mu.Lock()
		if _, exists := reasonsMap[info.ID]; !exists {
			reasonsMap[info.ID] = make(types.Reasons[enum.UserReasonType])
		}
		reasonsMap[info.ID].Add(enum.UserReasonTypeOutfit, &types.Reason{
			Message:    "User has outfits with inappropriate themes.",
			Confidence: highestConfidence,
			Evidence:   allSuspiciousThemes,
		})
		mu.Unlock()

		a.logger.Info("AI flagged user with outfit themes",
			zap.Uint64("userID", info.ID),
			zap.String("username", info.Name),
			zap.Float64("confidence", highestConfidence),
			zap.Int("numOutfits", len(allSuspiciousThemes)),
			zap.Strings("themes", allSuspiciousThemes))
	}

	return nil
}

// processOutfitBatch handles the AI analysis for a batch of outfit images.
func (a *OutfitAnalyzer) processOutfitBatch(
	ctx context.Context, info *types.User, batch []DownloadResult,
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

	// Add final user message with outfit names
	prompt := fmt.Sprintf(
		"%s\n\nIdentify themes for user %q.\nOutfit names: %s",
		OutfitRequestPrompt,
		info.Name,
		strings.Join(outfitNames, ", "),
	)
	messages = append(messages, openai.UserMessage(prompt))

	// Make API request with retry
	return utils.WithRetry(ctx, func() (*OutfitThemeAnalysis, error) {
		resp, err := a.chat.New(ctx, openai.ChatCompletionNewParams{
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
			Temperature: openai.Float(0.2),
			TopP:        openai.Float(0.1),
		})
		if err != nil {
			return nil, fmt.Errorf("openai API error: %w", err)
		}

		// Check for empty response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
			return nil, fmt.Errorf("%w: no response from model", ErrModelResponse)
		}

		// Extract thought process
		message := resp.Choices[0].Message
		if thought := message.JSON.ExtraFields["reasoning"]; thought.IsPresent() {
			a.logger.Debug("AI outfit analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response
		var analysis OutfitThemeAnalysis
		if err := sonic.Unmarshal([]byte(message.Content), &analysis); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		// Validate outfit names and filter out invalid ones
		var validThemes []OutfitTheme
		for _, theme := range analysis.Themes {
			if _, ok := validOutfits[theme.OutfitName]; ok {
				validThemes = append(validThemes, theme)
				continue
			}
			a.logger.Warn("AI flagged non-existent outfit",
				zap.String("username", info.Name),
				zap.String("outfitName", theme.OutfitName))
		}
		analysis.Themes = validThemes

		return &analysis, nil
	}, utils.GetAIRetryOptions())
}

// analyzeOutfitBatch processes a single batch of outfit images.
func (a *OutfitAnalyzer) analyzeOutfitBatch(
	ctx context.Context, info *types.User, downloads []DownloadResult,
) (*OutfitThemeAnalysis, error) {
	// Acquire semaphore
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Handle content blocking
	minBatchSize := max(len(downloads)/4, 1)
	return utils.WithRetrySplitBatch(
		ctx, downloads, len(downloads), minBatchSize,
		func(batch []DownloadResult) (*OutfitThemeAnalysis, error) {
			return a.processOutfitBatch(ctx, info, batch)
		},
		utils.GetAIRetryOptions(), a.logger,
	)
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
	ctx context.Context, userInfo *types.User, outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string,
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
			img, ok := a.downloadImage(ctx, thumbnailURL)
			if ok {
				mu.Lock()
				// Add current outfit at the beginning of the array
				downloads = append(downloads, DownloadResult{
					img:  img,
					name: "Current Outfit",
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
