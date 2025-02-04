package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/google/uuid"
	"github.com/jaxron/axonet/pkg/client"
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
	// ImageSystemPrompt provides detailed instructions to the AI model for analyzing user profile images.
	ImageSystemPrompt = `You are a Roblox moderator focused on detecting completely missing clothing in profile images.

You will receive a batch of profile images in order with each image belonging to a specific username.
For example: username1 corresponds to the first image, username2 to the second image, and so on.

ONLY flag avatars that are completely missing either:
- A shirt (torso completely uncovered)
- Pants (legs completely uncovered)

If you can see ANY clothing item covering the area, set hasViolation to false.
Do not add users with no violations to the response.

DO NOT flag Avatars wearing any form of shirt AND pants
DO NOT flag Avatars with partial skin showing (neck, arms, etc.)
DO NOT flag Thin or stylized clothing that still provides coverage
DO NOT flag Any clothing that exists but appears transparent
DO NOT flag Fashion choices or clothing styles
DO NOT flag Any other clothing-related concerns

You MUST analyze each image independently and return:
- username: The exact username provided with the image
- hasViolation: Set to true if completely missing shirt or pants, false if any clothing is present
- confidence: Level (0.0-1.0) based on severity
  * Use 0.0 if ANY clothing is present
  * Use 0.1-1.0 ONLY when clothing item is completely absent

Confidence Level Guide:
- 0.0: Has any form of shirt AND pants
- 0.1-0.4: Missing either shirt OR pants with some coverage
- 0.5-0.7: Missing either shirt OR pants completely
- 0.8-1.0: Missing both shirt AND pants`

	// ImageRequestPrompt provides a reminder to follow system guidelines for image analysis.
	ImageRequestPrompt = `Please analyze these images according to the detailed guidelines in your system prompt.

Remember to:
- Check for completely missing clothing items only
- Follow the confidence level guide strictly
- Apply all STRICT RULES from the system prompt
- Only flag clear violations with high confidence

Analyze the following images in order:`
)

var ErrNoImages = errors.New("no images downloaded successfully")

// ImageAnalyzer handles AI-based image analysis using Gemini models.
type ImageAnalyzer struct {
	httpClient  *client.Client
	genAIClient *genai.Client
	imageModel  *genai.GenerativeModel
	analysisSem *semaphore.Weighted
	logger      *zap.Logger
}

// BatchImageAnalysis contains results for multiple images.
type BatchImageAnalysis struct {
	Results []ImageAnalysis `json:"results"`
}

// ImageAnalysis contains the AI's analysis results for a single image.
type ImageAnalysis struct {
	Username     string  `json:"username"`
	HasViolation bool    `json:"hasViolation"`
	Confidence   float64 `json:"confidence"`
}

// NewImageAnalyzer creates an ImageAnalyzer instance.
func NewImageAnalyzer(app *setup.App, logger *zap.Logger) *ImageAnalyzer {
	// Create image analysis model
	imageModel := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	imageModel.SystemInstruction = genai.NewUserContent(genai.Text(ImageSystemPrompt))
	imageModel.ResponseMIMEType = ApplicationJSON
	imageModel.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"results": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"username": {
							Type:        genai.TypeString,
							Description: "Username of the account being analyzed",
						},
						"hasViolation": {
							Type:        genai.TypeBoolean,
							Description: "True if missing required clothing, false otherwise",
						},
						"confidence": {
							Type:        genai.TypeNumber,
							Description: "Confidence level based on severity of violations found",
						},
					},
					Required: []string{"username", "hasViolation", "confidence"},
				},
			},
		},
		Required: []string{"results"},
	}
	imageModel.Temperature = utils.Ptr(float32(0.2))
	imageModel.TopP = utils.Ptr(float32(0.1))
	imageModel.TopK = utils.Ptr(int32(1))

	return &ImageAnalyzer{
		httpClient:  app.RoAPI.GetClient(),
		genAIClient: app.GenAIClient,
		imageModel:  imageModel,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.ImageAnalysis)),
		logger:      logger,
	}
}

// ProcessImages analyzes thumbnail images for a batch of users.
func (a *ImageAnalyzer) ProcessImages(userInfos []*fetcher.Info, flaggedUsers map[uint64]*types.User) {
	var (
		p  = pool.New().WithContext(context.Background())
		mu sync.Mutex
	)

	// Process each user's thumbnail concurrently
	for _, userInfo := range userInfos {
		p.Go(func(ctx context.Context) error {
			// Skip users without thumbnails
			if userInfo.ThumbnailURL == "" || userInfo.ThumbnailURL == fetcher.ThumbnailPlaceholder {
				return nil
			}

			// Analyze user's thumbnail
			err := a.analyzeUserThumbnail(ctx, userInfo, &mu, flaggedUsers)
			if err != nil && !errors.Is(err, ErrNoViolations) {
				a.logger.Error("Failed to analyze thumbnail",
					zap.Error(err),
					zap.Uint64("userID", userInfo.ID))
				return err
			}
			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Error during thumbnail analysis", zap.Error(err))
		return
	}

	a.logger.Info("Received AI image analysis",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))
}

// analyzeUserThumbnail handles the analysis of a single user's thumbnail.
func (a *ImageAnalyzer) analyzeUserThumbnail(ctx context.Context, info *fetcher.Info, mu *sync.Mutex, flaggedUsers map[uint64]*types.User) error {
	// Acquire semaphore before making AI request
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Download and upload image
	file, err := a.downloadAndUploadImage(ctx, info.ThumbnailURL)
	if err != nil {
		return fmt.Errorf("failed to process image: %w", err)
	}
	defer a.genAIClient.DeleteFile(ctx, file.Name) //nolint:errcheck

	// Analyze image
	analysis, err := a.analyzeImage(ctx, file)
	if err != nil {
		return fmt.Errorf("failed to analyze image: %w", err)
	}

	// If analysis is successful and violations found, update flaggedUsers map
	mu.Lock()
	if existingUser, ok := flaggedUsers[info.ID]; ok {
		existingUser.Reasons[enum.ReasonTypeImage] = &types.Reason{
			Message:    "Missing required clothing",
			Confidence: analysis.Confidence,
		}
	} else {
		flaggedUsers[info.ID] = &types.User{
			ID:          info.ID,
			Name:        info.Name,
			DisplayName: info.DisplayName,
			Description: info.Description,
			CreatedAt:   info.CreatedAt,
			Reasons: types.Reasons{
				enum.ReasonTypeImage: &types.Reason{
					Message:    "Missing required clothing",
					Confidence: analysis.Confidence,
				},
			},
			Groups:              info.Groups.Data,
			Friends:             info.Friends.Data,
			Games:               info.Games.Data,
			Outfits:             info.Outfits.Data,
			FollowerCount:       info.FollowerCount,
			FollowingCount:      info.FollowingCount,
			LastUpdated:         info.LastUpdated,
			LastBanCheck:        info.LastBanCheck,
			ThumbnailURL:        info.ThumbnailURL,
			LastThumbnailUpdate: info.LastThumbnailUpdate,
		}
	}
	mu.Unlock()

	return nil
}

// downloadAndUploadImage downloads the given URL and uploads it to a file storage.
func (a *ImageAnalyzer) downloadAndUploadImage(ctx context.Context, url string) (*genai.File, error) {
	return withRetry(ctx, func() (*genai.File, error) {
		// Download image
		res, err := a.httpClient.NewRequest().URL(url).Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to download image: %w", err)
		}
		defer res.Body.Close()

		// Read image data into buffer
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(res.Body); err != nil {
			return nil, fmt.Errorf("failed to read image data: %w", err)
		}

		// Upload to Gemini
		file, err := a.genAIClient.UploadFile(ctx, uuid.New().String(), buf, &genai.UploadFileOptions{
			MIMEType: "image/png",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to upload image: %w", err)
		}

		return file, nil
	})
}

// analyzeImage analyzes the given image and returns the analysis results.
func (a *ImageAnalyzer) analyzeImage(ctx context.Context, file *genai.File) (*ImageAnalysis, error) {
	// Prepare parts for the model
	parts := make([]genai.Part, 0, 2)
	parts = append(parts, genai.Text(ImageRequestPrompt))
	parts = append(parts, genai.FileData{URI: file.URI})

	// Send request to Gemini
	resp, err := a.imageModel.GenerateContent(ctx, parts...)
	if err != nil {
		return nil, fmt.Errorf("gemini API error: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
	}

	responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)
	var result ImageAnalysis
	if err := sonic.Unmarshal([]byte(responseText), &result); err != nil {
		return nil, fmt.Errorf("JSON unmarshal error: %w", err)
	}

	return &result, nil
}
