package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
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
	"github.com/robalyx/rotector/internal/common/utils"
	"go.uber.org/zap"
	"golang.org/x/image/webp"
	"golang.org/x/sync/semaphore"
)

const (
	// OutfitSystemPrompt provides detailed instructions to the AI model for analyzing user outfits
	OutfitSystemPrompt = `You are a Roblox moderator focused on detecting outfits that may indicate predatory behavior targeting minors.

You will receive:
1. A username
2. Names of up to 16 outfits
3. A grid image showing these outfits

Analyze the outfits for concerning content including:
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
- confidence: Level (0.0-1.0) based on severity
  * Use 0.0 for no violations
  * Use 0.1-1.0 ONLY when clear violations exist`
)

const (
	MaxOutfits  = 16
	GridColumns = 4
	GridSize    = 150
)

var (
	ErrNoViolations = errors.New("no violations found in outfits")
	ErrNoOutfits    = errors.New("no outfit images downloaded successfully")
)

// OutfitAnalysis contains the AI's analysis results for a user's outfits.
type OutfitAnalysis struct {
	Username   string  `json:"username"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
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
			"confidence": {
				Type:        genai.TypeNumber,
				Description: "Confidence level based on severity of violations found",
			},
		},
		Required: []string{"username", "reason", "confidence"},
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
// Returns IDs of users that failed validation for retry.
func (a *OutfitAnalyzer) ProcessOutfits(userInfos []*fetcher.Info, flaggedUsers map[uint64]*types.User) ([]uint64, error) {
	type result struct {
		err    error
		userID uint64
	}

	var (
		resultsChan = make(chan result, len(userInfos))
		wg          sync.WaitGroup
		mu          sync.Mutex
	)

	// Process each user's outfits concurrently
	for _, userInfo := range userInfos {
		// Skip users with no outfits
		if len(userInfo.Outfits.Data) == 0 {
			continue
		}

		wg.Add(1)
		go func(info *fetcher.Info) {
			defer wg.Done()

			// Analyze user's outfits
			err := a.analyzeUserOutfits(context.Background(), info, &mu, flaggedUsers)
			if err != nil && !errors.Is(err, ErrNoViolations) {
				resultsChan <- result{
					err:    err,
					userID: info.ID,
				}
			}
		}(userInfo)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect failed validation IDs
	var failedValidationIDs []uint64
	for res := range resultsChan {
		if res.err != nil {
			failedValidationIDs = append(failedValidationIDs, res.userID)
			a.logger.Error("Failed to analyze outfits",
				zap.Error(res.err),
				zap.Uint64("userID", res.userID))
		}
	}

	a.logger.Info("Received AI outfit analysis",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return failedValidationIDs, nil
}

// analyzeUserOutfits handles the analysis of a single user's outfits.
func (a *OutfitAnalyzer) analyzeUserOutfits(ctx context.Context, info *fetcher.Info, mu *sync.Mutex, flaggedUsers map[uint64]*types.User) error {
	// Acquire semaphore before making AI request
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Analyze outfits with retry
	analysis, err := withRetry(ctx, func() (*OutfitAnalysis, error) {
		// Create grid image from outfits
		gridImage, outfitNames, err := a.createOutfitGrid(ctx, info)
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
		prompt := fmt.Sprintf("Analyze outfits for user %q.\nOutfit names: %v", info.Name, outfitNames)

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
		// Combine reasons and update confidence
		existingUser.Reason = fmt.Sprintf("%s\n\nOutfit Analysis: %s", existingUser.Reason, analysis.Reason)
		existingUser.Confidence = 1.0
	} else {
		flaggedUsers[info.ID] = &types.User{
			ID:                  info.ID,
			Name:                info.Name,
			DisplayName:         info.DisplayName,
			Description:         info.Description,
			CreatedAt:           info.CreatedAt,
			Reason:              "Outfit Analysis: " + analysis.Reason,
			Groups:              info.Groups.Data,
			Friends:             info.Friends.Data,
			Games:               info.Games.Data,
			Outfits:             info.Outfits.Data,
			FollowerCount:       info.FollowerCount,
			FollowingCount:      info.FollowingCount,
			Confidence:          analysis.Confidence,
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
func (a *OutfitAnalyzer) createOutfitGrid(ctx context.Context, userInfo *fetcher.Info) (*bytes.Buffer, []string, error) {
	// Get outfit thumbnails
	outfits, thumbnailMap := a.getOutfitThumbnails(ctx, userInfo.Outfits.Data)

	// Download outfit images concurrently
	successfulDownloads, err := a.downloadOutfitImages(ctx, outfits, thumbnailMap)
	if err != nil {
		return nil, nil, err
	}

	// Create and save grid image
	return a.createGridImage(successfulDownloads)
}

// getOutfitThumbnails fetches thumbnail URLs for outfits
func (a *OutfitAnalyzer) getOutfitThumbnails(ctx context.Context, outfits []*apiTypes.Outfit) ([]*apiTypes.Outfit, map[uint64]string) {
	if len(outfits) > MaxOutfits*2 {
		outfits = outfits[:MaxOutfits*2] // Allow for some failures
	}

	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, outfit := range outfits {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.OutfitType,
			TargetID:  outfit.ID,
			RequestID: strconv.FormatUint(outfit.ID, 10),
			Size:      apiTypes.Size150x150,
			Format:    apiTypes.WEBP,
		})
	}

	return outfits, a.thumbnailFetcher.ProcessBatchThumbnails(ctx, requests)
}

// downloadOutfitImages concurrently downloads outfit images until we have enough.
func (a *OutfitAnalyzer) downloadOutfitImages(ctx context.Context, outfits []*apiTypes.Outfit, thumbnailMap map[uint64]string) ([]DownloadResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg                  sync.WaitGroup
		results             = make(chan DownloadResult, len(outfits))
		successfulDownloads = make([]DownloadResult, 0, MaxOutfits)
		pendingDownloads    = 0
		mu                  sync.Mutex
	)

	for i := range outfits {
		// Check if thumbnail is valid
		thumbnailURL, ok := thumbnailMap[outfits[i].ID]
		if !ok || thumbnailURL == "" || thumbnailURL == fetcher.ThumbnailPlaceholder {
			continue
		}

		// Check if we have enough successful downloads
		mu.Lock()
		if len(successfulDownloads) >= MaxOutfits || pendingDownloads >= MaxOutfits {
			mu.Unlock()
			break
		}
		pendingDownloads++
		mu.Unlock()

		wg.Add(1)
		go func(index int, outfit *apiTypes.Outfit, url string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				mu.Lock()
				pendingDownloads--
				mu.Unlock()
				return
			default:
				// Download image
				resp, err := a.httpClient.NewRequest().URL(url).Do(ctx)
				if err != nil {
					a.logger.Warn("Failed to download outfit image",
						zap.Error(err),
						zap.String("url", url))
					mu.Lock()
					pendingDownloads--
					mu.Unlock()
					return
				}
				defer resp.Body.Close()

				// Decode image
				img, err := webp.Decode(resp.Body)
				if err != nil {
					mu.Lock()
					pendingDownloads--
					mu.Unlock()
					return
				}

				mu.Lock()
				if len(successfulDownloads) < MaxOutfits {
					results <- DownloadResult{index, img, outfit.Name}
				}
				pendingDownloads--
				mu.Unlock()
			}
		}(i, outfits[i], thumbnailURL)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect successful downloads until we have enough
	for result := range results {
		successfulDownloads = append(successfulDownloads, result)
		if len(successfulDownloads) >= MaxOutfits {
			cancel() // Cancel any remaining downloads
			break
		}
	}

	// Check if we got any successful downloads
	if len(successfulDownloads) == 0 {
		return nil, ErrNoOutfits
	}

	return successfulDownloads, nil
}

// createGridImage creates a grid from downloaded images.
func (a *OutfitAnalyzer) createGridImage(downloads []DownloadResult) (*bytes.Buffer, []string, error) {
	rows := (len(downloads) + GridColumns - 1) / GridColumns
	gridWidth := GridSize * GridColumns
	gridHeight := GridSize * rows

	dst := image.NewRGBA(image.Rect(0, 0, gridWidth, gridHeight))
	outfitNames := make([]string, len(downloads))

	// Draw images into grid
	for i, result := range downloads {
		// Store outfit name
		outfitNames[i] = result.name

		// Calculate position in grid
		x := (i % GridColumns) * GridSize
		y := (i / GridColumns) * GridSize

		// Draw image into grid
		draw.Draw(dst,
			image.Rect(x, y, x+GridSize, y+GridSize),
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
