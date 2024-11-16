package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jaxron/axonet/pkg/client"
	gim "github.com/ozankasikci/go-image-merge"
	"github.com/rotector/rotector/assets"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/queue"
	"golang.org/x/image/webp"
)

var (
	// ErrInvalidDateRangeFormat indicates that the date range string is not in the format "YYYY-MM-DD to YYYY-MM-DD".
	ErrInvalidDateRangeFormat = errors.New("invalid date range format")
	// ErrInvalidStartDate indicates that the start date could not be parsed from the provided string.
	ErrInvalidStartDate = errors.New("invalid start date")
	// ErrInvalidEndDate indicates that the end date could not be parsed from the provided string.
	ErrInvalidEndDate = errors.New("invalid end date")
	// ErrEndDateBeforeStartDate indicates that the end date occurs before the start date.
	ErrEndDateBeforeStartDate = errors.New("end date cannot be before start date")
)

// FormatIDs formats a slice of user IDs into a readable string with mentions.
func FormatIDs(ids []uint64) string {
	if len(ids) == 0 {
		return "None"
	}

	mentions := make([]string, len(ids))
	for i, id := range ids {
		mentions[i] = fmt.Sprintf("<@%d>", id)
	}
	return strings.Join(mentions, ", ")
}

// NormalizeString sanitizes text by replacing newlines with spaces and removing backticks
// to prevent Discord markdown formatting issues.
func NormalizeString(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "`", "")
}

// GetTimestampedSubtext formats a message with a Discord timestamp and prefix.
// The timestamp shows relative time (e.g., "2 minutes ago") using Discord's timestamp format.
func GetTimestampedSubtext(message string) string {
	if message != "" {
		return fmt.Sprintf("-# `%s` <t:%d:R>", message, time.Now().Unix())
	}
	return ""
}

// FormatNumber formats a number with K/M/B suffixes.
func FormatNumber(n uint64) string {
	if n < 1000 {
		return strconv.FormatUint(n, 10)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

// MergeImages combines multiple outfit thumbnails into a single grid image.
// It downloads images concurrently and uses a placeholder for missing or failed downloads.
// The grid dimensions are determined by the columns and rows parameters.
func MergeImages(client *client.Client, thumbnailURLs []string, columns, rows, perPage int) (*bytes.Buffer, error) {
	// Load placeholder image for missing or failed thumbnails
	imageFile, err := assets.Images.Open("images/content_deleted.png")
	if err != nil {
		return nil, err
	}
	defer func() { _ = imageFile.Close() }()

	placeholderImg, _, err := image.Decode(imageFile)
	if err != nil {
		return nil, err
	}

	// Initialize grid with empty images
	emptyImg := image.NewRGBA(image.Rect(0, 0, 150, 150))
	grids := make([]*gim.Grid, perPage)
	for i := range grids {
		grids[i] = &gim.Grid{Image: emptyImg}
	}

	// Download and process outfit images concurrently using goroutines
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
		if url == fetcher.ThumbnailPlaceholder {
			grids[i] = &gim.Grid{Image: placeholderImg}
			continue
		}

		wg.Add(1)
		go func(index int, imageURL string) {
			defer wg.Done()

			var img image.Image

			// Download and decode WebP image, use placeholder on failure
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

			// Thread-safe update of the grids slice
			mu.Lock()
			grids[index] = &gim.Grid{Image: img}
			mu.Unlock()
		}(i, url)
	}

	wg.Wait()

	// Merge all images into a single grid
	mergedImage, err := gim.New(grids, columns, rows).Merge()
	if err != nil {
		return nil, err
	}

	// Encode the final image as PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, mergedImage); err != nil {
		return nil, err
	}

	return &buf, nil
}

// CensorString partially obscures text by replacing middle characters with 'X'.
// The amount censored is 30% of the string length, centered in the middle.
// Strings of 2 characters or less are fully censored.
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

// GetMessageEmbedColor returns the appropriate embed color based on streamer mode.
// This helps visually distinguish when streamer mode is active.
func GetMessageEmbedColor(streamerMode bool) int {
	if streamerMode {
		return constants.StreamerModeEmbedColor
	}
	return constants.DefaultEmbedColor
}

// ParseDateRange converts a date range string into start and end time.Time values.
// The input format must be "YYYY-MM-DD to YYYY-MM-DD".
// The end date is automatically set to the end of the day (23:59:59).
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

// GetPriorityFromCustomID maps Discord component custom IDs to queue priority levels.
// Returns NormalPriority if the custom ID is not recognized.
func GetPriorityFromCustomID(customID string) string {
	switch customID {
	case constants.QueueHighPriorityCustomID:
		return queue.HighPriority
	case constants.QueueNormalPriorityCustomID:
		return queue.NormalPriority
	case constants.QueueLowPriorityCustomID:
		return queue.LowPriority
	default:
		return queue.NormalPriority
	}
}
