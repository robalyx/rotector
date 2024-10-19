package utils

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/invopop/jsonschema"
	"github.com/jaxron/axonet/pkg/client"
	gim "github.com/ozankasikci/go-image-merge"
	"github.com/rotector/rotector/assets"
	"golang.org/x/image/webp"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalizer is a transform.Transformer that normalizes a string.
var normalizer = transform.Chain( //nolint:gochecknoglobals
	norm.NFKC,
	norm.NFD,
	runes.Remove(runes.In(unicode.Mn)),
	runes.Remove(runes.In(unicode.Space)),
	cases.Lower(language.Und),
	norm.NFC,
)

// GenerateSchema generates a JSON schema for the given type.
func GenerateSchema[T any]() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}

// GetTimestampedSubtext returns a timestamped subtext message.
func GetTimestampedSubtext(message string) string {
	if message != "" {
		return fmt.Sprintf("-# `%s` <t:%d:R>", message, time.Now().Unix())
	}
	return ""
}

// NormalizeString removes diacritics, spaces, and converts to lowercase.
func NormalizeString(s string) string {
	result, _, _ := transform.String(normalizer, s)
	return result
}

// ContainsNormalized checks if substr is in s, after normalizing both.
func ContainsNormalized(s, substr string) bool {
	if s == "" || substr == "" {
		return false
	}
	return strings.Contains(NormalizeString(s), NormalizeString(substr))
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
