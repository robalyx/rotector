package export

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// HashType represents the different hashing algorithms available.
type HashType string

const (
	// HashTypeArgon2id uses the Argon2id algorithm for hashing.
	HashTypeArgon2id HashType = "argon2id"
	// HashTypeSHA256 uses the SHA256 algorithm for hashing.
	HashTypeSHA256 HashType = "sha256"
)

// HashResult represents a hashed ID with its index.
type HashResult struct {
	Index int
	Hash  string
}

// HashID converts a single ID to a hash using the specified algorithm with the provided salt.
func HashID(id int64, salt string, hashType HashType, iterations uint32, memory uint32) string {
	// Convert ID to bytes in little-endian format
	idBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(idBytes, uint64(id)) //nolint:gosec // unlikely to overflow

	var hash []byte

	switch hashType {
	case HashTypeArgon2id:
		// Use Argon2id with specified parameters
		hash = argon2.IDKey(idBytes, []byte(salt), iterations, memory*1024, 1, 32)
	case HashTypeSHA256:
		// Iterative SHA256 hashing with salt
		hash = []byte(salt)

		h := sha256.New()
		for range iterations {
			h.Reset()
			h.Write(idBytes)
			h.Write(hash)
			hash = h.Sum(nil)
		}
	}

	return hex.EncodeToString(hash)
}

// hashIDs concurrently hashes multiple IDs.
func hashIDs(ids []int64, salt string, hashType HashType, concurrency int64, iterations, memory uint32) []string {
	if len(ids) == 0 {
		return nil
	}

	// Check if concurrency is valid
	if concurrency < 1 {
		concurrency = 1
	}
	// Limit concurrency to number of IDs
	count := int64(len(ids))
	if concurrency > count {
		concurrency = count
	}

	// Create channels for work distribution and results
	work := make(chan int, count)
	results := make(chan HashResult, count)
	progress := make(chan struct{}, count)

	// Start worker pool
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for idx := range work {
				hash := HashID(ids[idx], salt, hashType, iterations, memory)
				results <- HashResult{idx, hash}

				progress <- struct{}{}
			}
		}()
	}

	// Send work to workers
	go func() {
		for i := range ids {
			work <- i
		}

		close(work)
	}()

	// Print progress with ETA
	go func() {
		processed := 0
		total := len(ids)
		start := time.Now()

		fmt.Printf("  0/%d (0%%) ETA: calculating...", total)

		for range progress {
			processed++
			percent := (processed * 100) / total

			// Calculate ETA
			elapsed := time.Since(start)
			if processed > 0 {
				avgTimePerHash := elapsed / time.Duration(processed)

				remaining := time.Duration(total-processed) * avgTimePerHash
				if remaining < time.Second {
					fmt.Printf("\r  %d/%d (%d%%) ETA: <1s        ", processed, total, percent)
				} else {
					fmt.Printf("\r  %d/%d (%d%%) ETA: %s        ", processed, total, percent, formatDuration(remaining))
				}
			}

			if processed == total {
				elapsed := time.Since(start)
				fmt.Printf("\r  %d/%d (%d%%) Time: %s        \n", processed, total, percent, formatDuration(elapsed))
			}
		}
	}()

	// Wait for all workers to finish and close channels
	go func() {
		wg.Wait()
		close(results)
		close(progress)
	}()

	// Collect results in order
	hashes := make([]string, len(ids))
	for r := range results {
		hashes[r.Index] = r.Hash
	}

	return hashes
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	hours := int64(d.Hours())
	minutes := int64(d.Minutes()) % 60
	seconds := int64(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	}

	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}

	return fmt.Sprintf("%ds", seconds)
}
