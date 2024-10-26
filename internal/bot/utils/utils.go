package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/axonet/pkg/client"
	gim "github.com/ozankasikci/go-image-merge"
	"github.com/rotector/rotector/assets"
	"github.com/rotector/rotector/internal/bot/constants"
	"golang.org/x/image/webp"
)

var (
	ErrInvalidDateRangeFormat = errors.New("invalid date range format")
	ErrInvalidStartDate       = errors.New("invalid start date")
	ErrInvalidEndDate         = errors.New("invalid end date")
	ErrEndDateBeforeStartDate = errors.New("end date cannot be before start date")
)

// FormatWhitelistedRoles formats the whitelisted roles.
func FormatWhitelistedRoles(whitelistedRoles []uint64, roles []discord.Role) string {
	var roleNames []string
	for _, roleID := range whitelistedRoles {
		for _, role := range roles {
			if uint64(role.ID) == roleID {
				roleNames = append(roleNames, role.Name)
				break
			}
		}
	}
	if len(roleNames) == 0 {
		return "No roles whitelisted"
	}
	return strings.Join(roleNames, ", ")
}

// NormalizeString normalizes a string by removing all newlines and backticks.
func NormalizeString(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "`", "")
}

// GetTimestampedSubtext returns a timestamped subtext message.
func GetTimestampedSubtext(message string) string {
	if message != "" {
		return fmt.Sprintf("-# `%s` <t:%d:R>", message, time.Now().Unix())
	}
	return ""
}

// MergeImages merges images from thumbnail URLs.
func MergeImages(client *client.Client, thumbnailURLs []string, columns, rows, perPage int) (*bytes.Buffer, error) {
	// Load placeholder image
	imageFile, err := assets.Images.Open("images/content_deleted.png")
	if err != nil {
		return nil, err
	}
	defer func() { _ = imageFile.Close() }()

	placeholderImg, _, err := image.Decode(imageFile)
	if err != nil {
		return nil, err
	}

	// Load grids with empty image
	emptyImg := image.NewRGBA(image.Rect(0, 0, 150, 150))

	grids := make([]*gim.Grid, perPage)
	for i := range grids {
		grids[i] = &gim.Grid{Image: emptyImg}
	}

	// Download and process outfit images concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, url := range thumbnailURLs {
		// Ensure we don't exceed the perPage limit
		if i >= perPage {
			break
		}

		// Skip if the URL is empty
		if url == "" {
			continue
		}

		// Skip if the URL was in a different state
		if url == "-" {
			grids[i] = &gim.Grid{Image: placeholderImg}
			continue
		}

		wg.Add(1)
		go func(index int, imageURL string) {
			defer wg.Done()

			var img image.Image

			// Download image
			resp, err := client.NewRequest().URL(imageURL).Do(context.Background())
			if err != nil {
				img = placeholderImg
			} else {
				defer resp.Body.Close()
				decodedImg, err := webp.Decode(resp.Body)
				if err != nil {
					img = placeholderImg
				} else {
					img = decodedImg
				}
			}

			// Safely update the grids slice
			mu.Lock()
			grids[index] = &gim.Grid{Image: img}
			mu.Unlock()
		}(i, url)
	}

	wg.Wait()

	// Merge images
	mergedImage, err := gim.New(grids, columns, rows).Merge()
	if err != nil {
		return nil, err
	}

	// Encode the merged image to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, mergedImage); err != nil {
		return nil, err
	}

	return &buf, nil
}

// CensorString censors a string based on streamer mode.
func CensorString(s string, streamerMode bool) string {
	// If streamer mode is off, return the original string
	if !streamerMode {
		return s
	}

	// Convert string to runes for proper Unicode handling
	runes := []rune(s)
	length := len(runes)

	// Censor entire string if it's 2 characters or less
	if length <= 2 {
		return strings.Repeat("X", length)
	}

	// Calculate the length to censor (30% of the string)
	censorLength := int(math.Ceil(float64(length) * 0.3))

	// Determine the start and end positions for censoring
	startCensor := (length - censorLength) / 2
	endCensor := startCensor + censorLength

	// Replace middle characters with 'X'
	for i := startCensor; i < endCensor && i < length; i++ {
		runes[i] = 'X'
	}

	// Convert back to string and return
	return string(runes)
}

// GetMessageEmbedColor returns the color of the message embed based on streamer mode.
func GetMessageEmbedColor(streamerMode bool) int {
	if streamerMode {
		return constants.StreamerModeEmbedColor
	}
	return constants.DefaultEmbedColor
}

// ParseDateRange parses a date range string in the format "YYYY-MM-DD to YYYY-MM-DD".
func ParseDateRange(dateRangeStr string) (time.Time, time.Time, error) {
	// Split the date range string into start and end parts
	parts := strings.Split(dateRangeStr, "to")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, ErrInvalidDateRangeFormat
	}

	// Trim spaces from the start and end parts
	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])

	// Parse the start date
	startDate, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %w", ErrInvalidStartDate, err)
	}

	// Parse the end date
	endDate, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %w", ErrInvalidEndDate, err)
	}

	// If the end date is before the start date, return an error
	if endDate.Before(startDate) {
		return time.Time{}, time.Time{}, ErrEndDateBeforeStartDate
	}

	// If the dates are the same, set the end date to the end of the day
	if startDate.Equal(endDate) {
		endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	} else {
		// Otherwise, set the end date to the end of the day
		endDate = endDate.Add(24*time.Hour - 1*time.Second)
	}

	return startDate, endDate, nil
}
