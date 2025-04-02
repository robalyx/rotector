package utils_test

import (
	"testing"

	"github.com/robalyx/rotector/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestPtr(t *testing.T) {
	t.Parallel()
	t.Run("string pointer", func(t *testing.T) {
		t.Parallel()
		s := "test"
		ptr := utils.Ptr(s)
		assert.NotNil(t, ptr)
		assert.Equal(t, s, *ptr)
	})

	t.Run("integer pointer", func(t *testing.T) {
		t.Parallel()
		i := 42
		ptr := utils.Ptr(i)
		assert.NotNil(t, ptr)
		assert.Equal(t, i, *ptr)
	})

	t.Run("boolean pointer", func(t *testing.T) {
		t.Parallel()
		b := true
		ptr := utils.Ptr(b)
		assert.NotNil(t, ptr)
		assert.Equal(t, b, *ptr)
	})

	t.Run("struct pointer", func(t *testing.T) {
		t.Parallel()
		type testStruct struct {
			Field string
		}
		s := testStruct{Field: "value"}
		ptr := utils.Ptr(s)
		assert.NotNil(t, ptr)
		assert.Equal(t, s, *ptr)
	})
}
