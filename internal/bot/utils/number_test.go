package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		name string
		n    uint64
		want string
	}{
		{
			name: "small number",
			n:    123,
			want: "123",
		},
		{
			name: "thousands",
			n:    1234,
			want: "1.2K",
		},
		{
			name: "millions",
			n:    1234567,
			want: "1.2M",
		},
		{
			name: "billions",
			n:    1234567890,
			want: "1.2B",
		},
		{
			name: "zero",
			n:    0,
			want: "0",
		},
		{
			name: "exact thousand",
			n:    1000,
			want: "1.0K",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatNumber(tt.n)
			assert.Equal(t, tt.want, got)
		})
	}
}
