package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/google/uuid"
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
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
	"golang.org/x/sync/semaphore"
)

const (
	// OutfitSystemPrompt provides detailed instructions to the AI model for analyzing user outfits
	OutfitSystemPrompt = `You are a Roblox moderator focused on detecting outfits that may indicate predatory behavior targeting minors.

You will receive:
1. A username
2. A grid image showing outfits where:
   - Index 0 is the user's current profile thumbnail
   - Remaining indices are the user's saved outfits
3. Names of the outfits (index 0 is "Current Profile Image")

Analyze both the current profile image and outfits for concerning content including:
- Outfits missing either a shirt OR pants
- Outfits designed to appear unclothed or suggestive
- Outfits with names containing inappropriate references
- Outfits with names suggesting adult content

If you can see ANY clothing item fully covering the area, do not flag the outfit.
Do not add users with no violations to the response.

DO NOT flag Avatars with partial skin showing (neck, arms, etc.)
DO NOT flag Thin or stylized clothing that still provides coverage
DO NOT flag Any clothing that exists but appears transparent
DO NOT flag Fashion choices or clothing styles
DO NOT flag Any other clothing-related concerns

Return:
- username: The exact username provided
- reason: Clear explanation of violations found in one sentence. Use exactly "NO_VIOLATIONS" if no clear concerns found
- evidence: Array of outfit names that have violations
- confidence: Level (0.0-1.0) based on severity
  * Use 0.0 for no violations
  * Use 0.1-1.0 ONLY when clear violations exist`

	// OutfitRequestPrompt provides a reminder to follow system guidelines for outfit analysis.
	OutfitRequestPrompt = `Please analyze these outfits according to the detailed guidelines in your system prompt.

Remember to:
- Check for missing clothing and inappropriate outfit designs
- Consider outfit names for concerning content
- Follow the confidence level guide strictly
- Apply all STRICT RULES from the system prompt
- Only flag clear violations with high confidence`
)

const (
	MaxOutfits        = 15
	OutfitGridColumns = 4
	OutfitGridSize    = 150
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
	genAIClient      *genai.Client
	outfitModel      *genai.GenerativeModel
	thumbnailFetcher *fetcher.ThumbnailFetcher
	analysisSem      *semaphore.Weighted
	logger           *zap.Logger
}

// DownloadResult contains the result of a single outfit image download.
type DownloadResult struct {
	index int
	img   image.Image
	name  string
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
		genAIClient:      app.GenAIClient,
		outfitModel:      outfitModel,
		thumbnailFetcher: fetcher.NewThumbnailFetcher(app.RoAPI, logger),
		analysisSem:      semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.OutfitAnalysis)),
		logger:           logger,
	}
}

// ProcessOutfits analyzes outfit images for a batch of users.
func (a *OutfitAnalyzer) ProcessOutfits(userInfos []*fetcher.Info, flaggedUsers map[uint64]*types.User) {
	// Track counts before processing
	existingFlags := len(flaggedUsers)

	// Get all outfit thumbnails organized by user
	userOutfits, userThumbnails := a.getOutfitThumbnails(context.Background(), userInfos)

	// Process each user's outfits concurrently
	var (
		p  = pool.New().WithContext(context.Background())
		mu sync.Mutex
	)

	for _, userInfo := range userInfos {
		// Skip if user has no outfits
		outfits, hasOutfits := userOutfits[userInfo.ID]
		if !hasOutfits {
			continue
		}

		thumbnails := userThumbnails[userInfo.ID]

		p.Go(func(ctx context.Context) error {
			// Analyze user's outfits
			err := a.analyzeUserOutfits(ctx, userInfo, &mu, flaggedUsers, outfits, thumbnails)
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

	a.logger.Info("Received AI outfit analysis",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("newFlags", len(flaggedUsers)-existingFlags))
}

// getOutfitThumbnails fetches thumbnail URLs for outfits and organizes them by user.
func (a *OutfitAnalyzer) getOutfitThumbnails(ctx context.Context, userInfos []*fetcher.Info) (map[uint64][]*apiTypes.Outfit, map[uint64]map[uint64]string) {
	// Collect all outfits from all users
	allOutfits := make([]*apiTypes.Outfit, 0)
	outfitToUser := make(map[uint64]*fetcher.Info)

	for _, userInfo := range userInfos {
		// Skip users with no outfits
		if len(userInfo.Outfits.Data) == 0 {
			continue
		}

		// Limit outfits per user
		userOutfits := userInfo.Outfits.Data
		if len(userOutfits) > MaxOutfits {
			userOutfits = userOutfits[:MaxOutfits]
		}

		// Add outfits to collection and map them back to user
		for _, outfit := range userOutfits {
			allOutfits = append(allOutfits, outfit)
			outfitToUser[outfit.ID] = userInfo
		}
	}

	// Build batch request for all outfits
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, outfit := range allOutfits {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.OutfitType,
			TargetID:  outfit.ID,
			RequestID: strconv.FormatUint(outfit.ID, 10),
			Size:      apiTypes.Size150x150,
			Format:    apiTypes.WEBP,
		})
	}

	// Get thumbnails for all outfits
	thumbnailMap := a.thumbnailFetcher.ProcessBatchThumbnails(ctx, requests)

	// Group outfits and thumbnails by user
	userOutfits := make(map[uint64][]*apiTypes.Outfit)
	userThumbnails := make(map[uint64]map[uint64]string)

	for _, outfit := range allOutfits {
		if userInfo, exists := outfitToUser[outfit.ID]; exists {
			userOutfits[userInfo.ID] = append(userOutfits[userInfo.ID], outfit)
			if userThumbnails[userInfo.ID] == nil {
				userThumbnails[userInfo.ID] = make(map[uint64]string)
			}
			if thumbnailURL, ok := thumbnailMap[outfit.ID]; ok {
				userThumbnails[userInfo.ID][outfit.ID] = thumbnailURL
			}
		}
	}

	return userOutfits, userThumbnails
}

// analyzeUserOutfits handles the analysis of a single user's outfits.
func (a *OutfitAnalyzer) analyzeUserOutfits(ctx context.Context, info *fetcher.Info, mu *sync.Mutex, flaggedUsers map[uint64]*types.User, outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string) error {
	// Acquire semaphore before making AI request
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Analyze outfits with retry
	analysis, err := withRetry(ctx, func() (*OutfitAnalysis, error) {
		// Create grid image from outfits
		gridImage, outfitNames, err := a.createOutfitGrid(ctx, info, outfits, thumbnailMap)
		if err != nil {
			if errors.Is(err, ErrNoOutfits) {
				return nil, ErrNoViolations
			}
			return nil, fmt.Errorf("failed to create outfit grid: %w", err)
		}

		// Upload grid image to Gemini
		file, err := a.genAIClient.UploadFile(ctx, uuid.New().String(), gridImage, &genai.UploadFileOptions{
			MIMEType: "image/png",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to upload grid image: %w", err)
		}
		defer a.genAIClient.DeleteFile(ctx, file.Name) //nolint:errcheck

		// Prepare prompt with outfit information
		prompt := fmt.Sprintf("%s\n\nAnalyze outfits for user %q.\nOutfit names: %v", OutfitRequestPrompt, info.Name, outfitNames)

		// Send request to Gemini
		resp, err := a.outfitModel.GenerateContent(ctx,
			genai.Text(prompt),
			genai.FileData{URI: file.URI},
		)
		if err != nil {
			return nil, fmt.Errorf("gemini API error: %w", err)
		}

		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
		}

		responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)
		var result OutfitAnalysis
		if err := sonic.Unmarshal([]byte(responseText), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return &result, nil
	})
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

	// If analysis is successful and violations found, update flaggedUsers map
	mu.Lock()
	if existingUser, ok := flaggedUsers[info.ID]; ok {
		existingUser.Reasons[enum.ReasonTypeOutfit] = &types.Reason{
			Message:    analysis.Reason,
			Confidence: analysis.Confidence,
			Evidence:   analysis.Evidence,
		}
	} else {
		flaggedUsers[info.ID] = &types.User{
			ID:          info.ID,
			Name:        info.Name,
			DisplayName: info.DisplayName,
			Description: info.Description,
			CreatedAt:   info.CreatedAt,
			Reasons: types.Reasons{
				enum.ReasonTypeOutfit: &types.Reason{
					Message:    analysis.Reason,
					Confidence: analysis.Confidence,
					Evidence:   analysis.Evidence,
				},
			},
			Groups:              info.Groups.Data,
			Friends:             info.Friends.Data,
			Games:               info.Games.Data,
			Outfits:             info.Outfits.Data,
			LastUpdated:         info.LastUpdated,
			LastBanCheck:        info.LastBanCheck,
			ThumbnailURL:        info.ThumbnailURL,
			LastThumbnailUpdate: info.LastThumbnailUpdate,
		}
	}
	mu.Unlock()

	return nil
}

// createOutfitGrid downloads outfit images and creates a grid image
func (a *OutfitAnalyzer) createOutfitGrid(ctx context.Context, userInfo *fetcher.Info, outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string) (*bytes.Buffer, []string, error) {
	// Download outfit images concurrently
	successfulDownloads, err := a.downloadOutfitImages(ctx, userInfo, outfits, thumbnailMap)
	if err != nil {
		return nil, nil, err
	}

	// Create and save grid image
	return a.createGridImage(successfulDownloads)
}

// downloadOutfitImages concurrently downloads outfit images until we have enough.
func (a *OutfitAnalyzer) downloadOutfitImages(ctx context.Context, userInfo *fetcher.Info, outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string) ([]DownloadResult, error) {
	// Download current user thumbnail
	downloads := make([]DownloadResult, 0, MaxOutfits)
	thumbnailURL := userInfo.ThumbnailURL
	if thumbnailURL != "" && thumbnailURL != fetcher.ThumbnailPlaceholder {
		thumbnailImg, ok := a.downloadImage(ctx, thumbnailURL)
		if ok {
			downloads = append(downloads, DownloadResult{
				index: 0,
				img:   thumbnailImg,
				name:  "Current Profile Image",
			})
		}
	}

	// Process outfits concurrently
	var (
		p  = pool.New().WithContext(ctx)
		mu sync.Mutex
	)

	for i := range outfits {
		// Check if thumbnail is valid
		thumbnailURL, ok := thumbnailMap[outfits[i].ID]
		if !ok || thumbnailURL == "" || thumbnailURL == fetcher.ThumbnailPlaceholder {
			continue
		}

		currentIndex := i + 1
		currentOutfit := outfits[i]
		currentURL := thumbnailURL

		p.Go(func(ctx context.Context) error {
			img, ok := a.downloadImage(ctx, currentURL)
			if !ok {
				return nil
			}

			mu.Lock()
			downloads = append(downloads, DownloadResult{
				index: currentIndex,
				img:   img,
				name:  currentOutfit.Name,
			})
			mu.Unlock()

			return nil
		})
	}

	// Wait for all downloads to complete
	if err := p.Wait(); err != nil && !errors.Is(err, ErrInvalidThumbnailURL) {
		a.logger.Error("Error during outfit downloads", zap.Error(err))
	}

	// Check if we got any successful downloads
	if len(downloads) == 0 {
		return nil, ErrNoOutfits
	}

	return downloads, nil
}

// createGridImage creates a grid from downloaded images.
func (a *OutfitAnalyzer) createGridImage(downloads []DownloadResult) (*bytes.Buffer, []string, error) {
	rows := (len(downloads) + OutfitGridColumns - 1) / OutfitGridColumns
	gridWidth := OutfitGridSize * OutfitGridColumns
	gridHeight := OutfitGridSize * rows

	dst := image.NewRGBA(image.Rect(0, 0, gridWidth, gridHeight))
	outfitNames := make([]string, len(downloads))

	// Draw images into grid
	for i, result := range downloads {
		// Store outfit name
		outfitNames[i] = result.name

		// Calculate position in grid
		x := (i % OutfitGridColumns) * OutfitGridSize
		y := (i / OutfitGridColumns) * OutfitGridSize

		// Draw image into grid
		draw.Draw(dst,
			image.Rect(x, y, x+OutfitGridSize, y+OutfitGridSize),
			result.img,
			image.Point{},
			draw.Over)
	}

	// Encode final grid image
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, dst); err != nil {
		return nil, nil, fmt.Errorf("failed to encode grid image: %w", err)
	}

	return buf, outfitNames, nil
}

// downloadImage downloads an image from a URL and resizes it to 150x150 if needed.
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
	img, err := webp.Decode(resp.Body)
	if err != nil {
		return nil, false
	}

	// Check if image needs resizing
	bounds := img.Bounds()
	if bounds.Dx() != 150 || bounds.Dy() != 150 {
		// Create a new 150x150 RGBA image
		resized := image.NewRGBA(image.Rect(0, 0, 150, 150))

		// Draw the original image into the resized image
		draw.NearestNeighbor.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)

		return resized, true
	}

	return img, true
}
