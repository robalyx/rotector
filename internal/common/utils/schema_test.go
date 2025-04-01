package utils_test

import (
	"encoding/json"
	"testing"

	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type SimpleStruct struct {
	Name    string `json:"name"`
	Age     int    `json:"age"`
	IsValid bool   `json:"isValid"`
}

type NestedStruct struct {
	ID   string       `json:"id"`
	Data SimpleStruct `json:"data"`
	Tags []string     `json:"tags"`
}

func TestGenerateSchema(t *testing.T) {
	t.Parallel()

	t.Run("simple struct schema", func(t *testing.T) {
		t.Parallel()
		schema := utils.GenerateSchema[SimpleStruct]()

		// Convert to JSON for easier assertion
		schemaBytes, err := json.Marshal(schema)
		require.NoError(t, err)

		var schemaMap map[string]any
		err = json.Unmarshal(schemaBytes, &schemaMap)
		require.NoError(t, err)

		// Check schema structure
		assert.Equal(t, "object", schemaMap["type"])
		properties := schemaMap["properties"].(map[string]any)

		// Check name property
		nameProps := properties["name"].(map[string]any)
		assert.Equal(t, "string", nameProps["type"])

		// Check age property
		ageProps := properties["age"].(map[string]any)
		assert.Equal(t, "integer", ageProps["type"])

		// Check isValid property
		isValidProps := properties["isValid"].(map[string]any)
		assert.Equal(t, "boolean", isValidProps["type"])
	})

	t.Run("nested struct schema", func(t *testing.T) {
		t.Parallel()
		schema := utils.GenerateSchema[NestedStruct]()

		schemaBytes, err := json.Marshal(schema)
		require.NoError(t, err)

		var schemaMap map[string]any
		err = json.Unmarshal(schemaBytes, &schemaMap)
		require.NoError(t, err)

		// Check top-level structure
		assert.Equal(t, "object", schemaMap["type"])
		properties := schemaMap["properties"].(map[string]any)

		// Check ID field
		idProps := properties["id"].(map[string]any)
		assert.Equal(t, "string", idProps["type"])

		// Check nested Data field
		dataProps := properties["data"].(map[string]any)
		assert.Equal(t, "object", dataProps["type"])

		// Check tags array
		tagsProps := properties["tags"].(map[string]any)
		assert.Equal(t, "array", tagsProps["type"])
		items := tagsProps["items"].(map[string]any)
		assert.Equal(t, "string", items["type"])
	})

	t.Run("primitive type schema", func(t *testing.T) {
		t.Parallel()
		schema := utils.GenerateSchema[string]()

		schemaBytes, err := json.Marshal(schema)
		require.NoError(t, err)

		var schemaMap map[string]any
		err = json.Unmarshal(schemaBytes, &schemaMap)
		require.NoError(t, err)

		assert.Equal(t, "string", schemaMap["type"])
	})
}
