package utils

import (
	"fmt"
	"strconv"
)

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
