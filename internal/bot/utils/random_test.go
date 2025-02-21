package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateRandomWords(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{
			name:  "single word",
			count: 1,
		},
		{
			name:  "multiple words",
			count: 3,
		},
		{
			name:  "zero words",
			count: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateRandomWords(tt.count)
			words := strings.Split(got, " ")

			if tt.count == 0 {
				assert.Empty(t, got)
			} else {
				assert.Len(t, words, tt.count)
			}
		})
	}
}
