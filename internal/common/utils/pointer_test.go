package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPtr(t *testing.T) {
	t.Run("string pointer", func(t *testing.T) {
		s := "test"
		ptr := Ptr(s)
		assert.NotNil(t, ptr)
		assert.Equal(t, s, *ptr)
	})

	t.Run("integer pointer", func(t *testing.T) {
		i := 42
		ptr := Ptr(i)
		assert.NotNil(t, ptr)
		assert.Equal(t, i, *ptr)
	})

	t.Run("boolean pointer", func(t *testing.T) {
		b := true
		ptr := Ptr(b)
		assert.NotNil(t, ptr)
		assert.Equal(t, b, *ptr)
	})

	t.Run("struct pointer", func(t *testing.T) {
		type testStruct struct {
			Field string
		}
		s := testStruct{Field: "value"}
		ptr := Ptr(s)
		assert.NotNil(t, ptr)
		assert.Equal(t, s, *ptr)
	})
}
