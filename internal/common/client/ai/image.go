package ai

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/google/uuid"
	"github.com/jaxron/axonet/pkg/client"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/utils"
	"go.uber.org/zap"
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
)

// ImageAnalyzer handles AI-based image analysis using Gemini models.
type ImageAnalyzer struct {
	httpClient  *client.Client
	genAIClient *genai.Client
	imageModel  *genai.GenerativeModel
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

// UserImage pairs a user's info with their uploaded image file.
type UserImage struct {
	info *fetcher.Info
	file *genai.File
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
		logger:      logger,
	}
}

// ProcessImages analyzes thumbnail images for a batch of users.
func (a *ImageAnalyzer) ProcessImages(userInfos []*fetcher.Info) (map[uint64]*types.User, []uint64, error) {
	var (
		ctx                 = context.Background()
		validatedUsers      = make(map[uint64]*types.User)
		userImages          []UserImage
		failedValidationIDs []uint64
	)

	// Analyze images with retry
	analysis, err := withRetry(ctx, func() (*BatchImageAnalysis, error) {
		// Download and upload images concurrently
		userImages, failedValidationIDs = a.downloadAndUploadImages(ctx, userInfos)
		defer a.cleanupFiles(ctx, userImages)

		// Prepare parts for the model
		parts := make([]genai.Part, 0, len(userImages)*2+1)
		parts = append(parts, genai.Text("Analyze the following images in order."))

		for _, img := range userImages {
			parts = append(parts, genai.Text(fmt.Sprintf("\nAnalyze image for username %q:", img.info.Name)))
			parts = append(parts, genai.FileData{URI: img.file.URI})
		}

		// Send request to Gemini
		resp, err := a.imageModel.GenerateContent(ctx, parts...)
		if err != nil {
			return nil, fmt.Errorf("gemini API error: %w", err)
		}

		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
		}

		responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)
		var result BatchImageAnalysis
		if err := sonic.Unmarshal([]byte(responseText), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return &result, nil
	})
	if err != nil {
		// If batch analysis fails, add all IDs to retry list
		for _, img := range userImages {
			failedValidationIDs = append(failedValidationIDs, img.info.ID)
		}
		a.logger.Error("Failed to analyze images", zap.Error(err))
		return validatedUsers, failedValidationIDs, err
	}

	// Create a map of username to user info
	usernameMap := make(map[string]*fetcher.Info)
	for _, img := range userImages {
		usernameMap[img.info.Name] = img.info
	}

	// Process results
	for _, result := range analysis.Results {
		userInfo, ok := usernameMap[result.Username]
		if !ok {
			a.logger.Error("AI returned result for unknown username",
				zap.String("username", result.Username))
			continue
		}

		// Skip results with no violations
		if !result.HasViolation {
			continue
		}

		// Validate confidence level
		if result.Confidence < 0.1 || result.Confidence > 1.0 {
			a.logger.Debug("AI flagged user with invalid confidence",
				zap.String("username", userInfo.Name),
				zap.Float64("confidence", result.Confidence))
			continue
		}

		validatedUsers[userInfo.ID] = &types.User{
			ID:                  userInfo.ID,
			Name:                userInfo.Name,
			DisplayName:         userInfo.DisplayName,
			Description:         userInfo.Description,
			CreatedAt:           userInfo.CreatedAt,
			Reason:              "Image Analysis: Missing required clothing",
			Groups:              userInfo.Groups.Data,
			Friends:             userInfo.Friends.Data,
			Games:               userInfo.Games.Data,
			FollowerCount:       userInfo.FollowerCount,
			FollowingCount:      userInfo.FollowingCount,
			Confidence:          result.Confidence,
			LastUpdated:         userInfo.LastUpdated,
			LastBanCheck:        userInfo.LastBanCheck,
			ThumbnailURL:        userInfo.ThumbnailURL,
			LastThumbnailUpdate: userInfo.LastThumbnailUpdate,
		}
	}

	a.logger.Info("Received AI image analysis",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("flaggedUsers", len(analysis.Results)),
		zap.Int("validatedUsers", len(validatedUsers)))

	return validatedUsers, failedValidationIDs, nil
}

// downloadAndUploadImages concurrently downloads and uploads images for a batch of users.
func (a *ImageAnalyzer) downloadAndUploadImages(ctx context.Context, userInfos []*fetcher.Info) ([]UserImage, []uint64) {
	type result struct {
		image UserImage
		err   error
	}

	// Initialize channels
	resultChan := make(chan result, len(userInfos))
	var failedValidationIDs []uint64
	var wg sync.WaitGroup

	// Process each user's thumbnail concurrently
	for _, userInfo := range userInfos {
		if userInfo.ThumbnailURL == "" || userInfo.ThumbnailURL == fetcher.ThumbnailPlaceholder {
			continue
		}

		wg.Add(1)
		currentInfo := userInfo

		go func() {
			defer wg.Done()

			file, err := a.downloadAndUploadImage(ctx, currentInfo.ThumbnailURL)
			if err != nil {
				a.logger.Warn("Failed to process image",
					zap.Uint64("userID", currentInfo.ID),
					zap.Error(err))
				resultChan <- result{err: err}
				return
			}

			resultChan <- result{
				image: UserImage{
					info: currentInfo,
					file: file,
				},
			}
		}()
	}

	// Close result channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	userImages := make([]UserImage, 0, len(userInfos))
	for res := range resultChan {
		if res.err != nil {
			if res.image.info != nil {
				failedValidationIDs = append(failedValidationIDs, res.image.info.ID)
			}
			continue
		}
		userImages = append(userImages, res.image)
	}

	return userImages, failedValidationIDs
}

// downloadAndUploadImage downloads the given URL and uploads it to a file storage.
func (a *ImageAnalyzer) downloadAndUploadImage(ctx context.Context, url string) (*genai.File, error) {
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
		MIMEType: "image/jpeg", // Roblox thumbnails are always JPEG
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload image: %w", err)
	}

	return file, nil
}

// cleanupFiles deletes uploaded files after analysis.
func (a *ImageAnalyzer) cleanupFiles(ctx context.Context, images []UserImage) {
	for _, img := range images {
		go func(file *genai.File) {
			if err := a.genAIClient.DeleteFile(ctx, file.Name); err != nil {
				a.logger.Warn("Failed to delete uploaded file",
					zap.String("fileName", file.Name),
					zap.Error(err))
			}
		}(img.file)
	}
}
