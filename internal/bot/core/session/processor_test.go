package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValueProcessor_ProcessValue(t *testing.T) {
	processor := NewValueProcessor()

	// Current time for testing time values
	now := time.Now().UTC().Truncate(time.Microsecond)

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: nil,
		},
		{
			name:  "uint64 value",
			input: uint64(18446744073709551615),
			expected: map[string]any{
				"value":                  "18446744073709551615",
				TypeMetadataPrefix + "0": NumericTypeMeta,
			},
		},
		{
			name:  "small uint64 value",
			input: uint64(42),
			expected: map[string]any{
				"value":                  "42",
				TypeMetadataPrefix + "0": NumericTypeMeta,
			},
		},
		{
			name:     "time.Time value",
			input:    now,
			expected: now.Format(time.RFC3339Nano),
		},
		{
			name:     "string value (unchanged)",
			input:    "test string",
			expected: "test string",
		},
		{
			name:     "int value (unchanged)",
			input:    42,
			expected: 42,
		},
		{
			name:     "float value (unchanged)",
			input:    42.5,
			expected: 42.5,
		},
		{
			name:     "bool value (unchanged)",
			input:    true,
			expected: true,
		},
		{
			name:  "slice of uint64",
			input: []uint64{1, 2, 18446744073709551615},
			expected: []any{
				map[string]any{"value": "1", TypeMetadataPrefix + "0": NumericTypeMeta},
				map[string]any{"value": "2", TypeMetadataPrefix + "0": NumericTypeMeta},
				map[string]any{"value": "18446744073709551615", TypeMetadataPrefix + "0": NumericTypeMeta},
			},
		},
		{
			name:  "slice of mixed types",
			input: []any{"string", uint64(123), 42, true},
			expected: []any{
				"string",
				map[string]any{"value": "123", TypeMetadataPrefix + "0": NumericTypeMeta},
				42,
				true,
			},
		},
		{
			name:  "map with string keys",
			input: map[string]any{"str": "value", "uint": uint64(123), "int": 42},
			expected: map[string]any{
				"str":  "value",
				"uint": map[string]any{"value": "123", TypeMetadataPrefix + "0": NumericTypeMeta},
				"int":  42,
			},
		},
		{
			name:  "nested map and slice",
			input: map[string]any{"items": []any{uint64(1), "test", map[string]any{"nestedUint": uint64(12345)}}},
			expected: map[string]any{
				"items": []any{
					map[string]any{"value": "1", TypeMetadataPrefix + "0": NumericTypeMeta},
					"test",
					map[string]any{
						"nestedUint": map[string]any{"value": "12345", TypeMetadataPrefix + "0": NumericTypeMeta},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processor.ProcessValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}

	t.Run("struct with fields", func(t *testing.T) {
		type TestStruct struct {
			ID        uint64
			Name      string
			CreatedAt time.Time
		}

		input := TestStruct{
			ID:        18446744073709551615,
			Name:      "Test",
			CreatedAt: now,
		}

		result := processor.ProcessValue(input)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map")

		// Check ID with metadata
		idValue, hasID := resultMap["ID"]
		require.True(t, hasID, "Expected to find ID field")
		idMap, isMap := idValue.(map[string]any)
		require.True(t, isMap, "Expected ID to be a map with metadata")
		assert.Equal(t, "18446744073709551615", idMap["value"])
		assert.Equal(t, NumericTypeMeta, idMap[TypeMetadataPrefix+"0"])

		// Check other fields
		assert.Equal(t, "Test", resultMap["Name"])
		assert.Equal(t, now.Format(time.RFC3339Nano), resultMap["CreatedAt"])
	})

	t.Run("pointer to uint64", func(t *testing.T) {
		val := uint64(18446744073709551615)
		ptr := &val
		result := processor.ProcessValue(ptr)

		// Check result is a map with metadata
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map with metadata")
		assert.Equal(t, "18446744073709551615", resultMap["value"])
		assert.Equal(t, NumericTypeMeta, resultMap[TypeMetadataPrefix+"0"])
	})
}

func TestValueProcessor_EmbeddedStructHandling(t *testing.T) {
	processor := NewValueProcessor()

	type Friend struct {
		ID uint64 `json:"id"`
	}

	type ExtendedFriend struct {
		Friend
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	}

	t.Run("EmbeddedStructFlattening", func(t *testing.T) {
		// Create test data with embedded struct
		friends := []*ExtendedFriend{
			{
				Friend:      Friend{ID: 2892328990},
				Name:        "Jake_oatmeal",
				DisplayName: "Jayden",
			},
			{
				Friend:      Friend{ID: 3900098213},
				Name:        "Consperson1",
				DisplayName: "KURRY",
			},
		}

		// Process the friends slice
		processedFriends := processor.ProcessValue(friends)

		// Convert back to JSON for verification
		jsonBytes, err := json.Marshal(processedFriends)
		require.NoError(t, err, "Expected to marshal processed friends to JSON")

		// Compare the resulting structure
		var result []map[string]any
		err = json.Unmarshal(jsonBytes, &result)
		require.NoError(t, err, "Expected to unmarshal JSON")

		// Verify that the ID field is flattened
		require.Equal(t, 2, len(result), "Expected 2 friends")
		require.Contains(t, result[0], "id", "Expected id field to be at top level")

		// Check that ID is now a map with type metadata
		idValue, ok := result[0]["id"].(map[string]any)
		require.True(t, ok, "Expected id to be a map with metadata")
		require.Equal(t, "2892328990", idValue["value"], "Expected ID value to be preserved")
		require.Equal(t, NumericTypeMeta, idValue[TypeMetadataPrefix+"0"], "Expected numeric type metadata")

		require.NotContains(t, result[0], "Friend", "Friend struct should be flattened")
	})

	t.Run("explicit JSON tag on embedded struct", func(t *testing.T) {
		type Base struct {
			ID uint64 `json:"id"`
		}

		type Tagged struct {
			Base `json:"base"` // Explicit JSON tag
			Name string        `json:"name"`
		}

		input := Tagged{
			Base: Base{ID: 12345},
			Name: "TestTagged",
		}

		result := processor.ProcessValue(input)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map")

		// The Base struct should not be flattened but kept under "base" key
		baseData, hasBase := resultMap["base"]
		require.True(t, hasBase, "Expected to find 'base' key for the embedded struct")

		baseMap, ok := baseData.(map[string]any)
		require.True(t, ok, "Expected base to be a map")

		// Check that ID is now a map with type metadata
		idValue, ok := baseMap["id"].(map[string]any)
		require.True(t, ok, "Expected id to be a map with metadata")
		require.Equal(t, "12345", idValue["value"], "Expected ID value to be preserved")
		require.Equal(t, NumericTypeMeta, idValue[TypeMetadataPrefix+"0"], "Expected numeric type metadata")

		assert.Equal(t, "TestTagged", resultMap["name"])
	})
}

func TestValueProcessor_EnumMapHandling(t *testing.T) {
	processor := NewValueProcessor()

	type ReasonType int
	const (
		ReasonTypeUser ReasonType = iota
		ReasonTypeFriend
		ReasonTypeOutfit
		ReasonTypeGroup
		ReasonTypeMember
		ReasonTypeCustom
	)

	type Reason struct {
		Message    string   `json:"message"`
		Confidence float64  `json:"confidence"`
		Evidence   []string `json:"evidence,omitempty"`
	}

	type Friend struct {
		ID uint64 `json:"id"`
	}

	type ExtendedFriend struct {
		Friend
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	}

	type TestUser struct {
		Reasons map[ReasonType]*Reason `json:"reasons"`
		Name    string                 `json:"name"`
		ID      uint64                 `json:"id"`
		Friends []*ExtendedFriend      `json:"friends"`
	}

	t.Run("EnumMapConversion", func(t *testing.T) {
		// Create test data with enum map keys
		original := map[ReasonType]*Reason{
			ReasonTypeUser: {
				Message:    "User has inappropriate content",
				Confidence: 0.85,
				Evidence:   []string{"profile text", "username"},
			},
			ReasonTypeFriend: {
				Message:    "User has flagged friends",
				Confidence: 0.75,
				Evidence:   []string{"friend1", "friend2"},
			},
			ReasonTypeOutfit: nil,
		}

		// Process with ValueProcessor
		processed := processor.ProcessValue(original)

		// Verify the keys were properly converted to strings containing numbers
		processedMap, ok := processed.(map[string]any)
		require.True(t, ok, "Expected processed result to be map[string]any")

		// Check that we have the expected keys with numeric string values
		require.Contains(t, processedMap, "0", "Expected to find key for ReasonTypeUser (0)")
		require.Contains(t, processedMap, "1", "Expected to find key for ReasonTypeFriend (1)")

		// Verify the values were preserved
		reason0, ok := processedMap["0"].(map[string]any)
		require.True(t, ok, "Expected reason to be map[string]any")
		require.Equal(t, "User has inappropriate content", reason0["message"])
		require.Equal(t, 0.85, reason0["confidence"])
	})

	t.Run("ComplexObjectWithEmbeddedAndMaps", func(t *testing.T) {
		// Create test data with embedded struct
		friends := []*ExtendedFriend{
			{
				Friend:      Friend{ID: 2892328990},
				Name:        "Jake_oatmeal",
				DisplayName: "Jayden",
			},
			{
				Friend:      Friend{ID: 3900098213},
				Name:        "Consperson1",
				DisplayName: "KURRY",
			},
		}

		// Create reasons map
		reasons := map[ReasonType]*Reason{
			ReasonTypeUser: {
				Message:    "User has inappropriate content",
				Confidence: 0.85,
				Evidence:   []string{"profile text", "username"},
			},
			ReasonTypeFriend: {
				Message:    "User has flagged friends",
				Confidence: 0.75,
				Evidence:   []string{"friend1", "friend2"},
			},
		}

		// Create test data with a user having friends and reasons
		user := &TestUser{
			Reasons: reasons,
			Name:    "TestUser",
			ID:      1234567890,
			Friends: friends,
		}

		// Process the user
		processedUser := processor.ProcessValue(user)
		userMap, ok := processedUser.(map[string]any)
		require.True(t, ok, "Expected processed user to be map[string]any")

		// Verify ID is properly converted
		idValue, ok := userMap["id"].(map[string]any)
		require.True(t, ok, "Expected id to be a map with metadata")
		require.Equal(t, "1234567890", idValue["value"], "Expected ID value to be preserved")
		require.Equal(t, NumericTypeMeta, idValue[TypeMetadataPrefix+"0"], "Expected numeric type metadata")

		// Verify name is preserved
		require.Equal(t, "TestUser", userMap["name"], "Expected Name to be preserved")

		// Verify reasons map exists and is correctly processed
		reasonsMap, ok := userMap["reasons"].(map[string]any)
		require.True(t, ok, "Expected reasons to be map[string]any")
		require.Contains(t, reasonsMap, "0", "Expected to find key for ReasonTypeUser (0)")

		// Verify reason content
		reasonObj, ok := reasonsMap["0"].(map[string]any)
		require.True(t, ok, "Expected reason to be map[string]any")
		require.Equal(t, "User has inappropriate content", reasonObj["message"], "Expected message to be preserved")
		require.Equal(t, 0.85, reasonObj["confidence"], "Expected confidence to be preserved")

		// Verify friends are properly processed
		friendsSlice, ok := userMap["friends"].([]any)
		require.True(t, ok, "Expected friends to be []any")
		require.Equal(t, 2, len(friendsSlice), "Expected 2 friends")

		// Verify first friend has flattened structure
		firstFriend, ok := friendsSlice[0].(map[string]any)
		require.True(t, ok, "Expected friend to be map[string]any")
		require.Contains(t, firstFriend, "id", "Expected id field to be at top level")

		// Check that ID is now a map with type metadata
		friendIdValue, ok := firstFriend["id"].(map[string]any)
		require.True(t, ok, "Expected id to be a map with metadata")
		require.Equal(t, "2892328990", friendIdValue["value"], "Expected ID value to be preserved")
		require.Equal(t, NumericTypeMeta, friendIdValue[TypeMetadataPrefix+"0"], "Expected numeric type metadata")

		require.NotContains(t, firstFriend, "Friend", "Friend struct should be flattened")
	})
}

func TestValueProcessor_JSONTagHandling(t *testing.T) {
	processor := NewValueProcessor()

	t.Run("json tag name", func(t *testing.T) {
		type TestStruct struct {
			RegularField string `json:"regular_field"`
			RenamedField string `json:"renamed"`
			OmittedField string `json:"-"`
			DefaultField string
		}

		input := TestStruct{
			RegularField: "regular",
			RenamedField: "renamed",
			OmittedField: "omitted",
			DefaultField: "default",
		}

		result := processor.ProcessValue(input)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map")

		assert.Equal(t, "regular", resultMap["regular_field"])
		assert.Equal(t, "renamed", resultMap["renamed"])
		assert.Equal(t, "default", resultMap["DefaultField"])
		assert.NotContains(t, resultMap, "OmittedField")
		assert.NotContains(t, resultMap, "-")
	})

	t.Run("json tag options", func(t *testing.T) {
		type TestStruct struct {
			RequiredField       string `json:"required_field,required"`
			OmitemptyField      string `json:"omitempty_field,omitempty"`
			EmptyOmitemptyField string `json:"empty_omitempty,omitempty"`
		}

		input := TestStruct{
			RequiredField:       "required",
			OmitemptyField:      "not empty",
			EmptyOmitemptyField: "",
		}

		result := processor.ProcessValue(input)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map")

		// Tag options should be ignored by our processor, we just extract the field name
		assert.Equal(t, "required", resultMap["required_field"])
		assert.Equal(t, "not empty", resultMap["omitempty_field"])
		assert.Equal(t, "", resultMap["empty_omitempty"])
	})
}

func BenchmarkValueProcessor_ProcessValue(b *testing.B) {
	processor := NewValueProcessor()

	benchmarks := []struct {
		name  string
		input any
	}{
		{
			name:  "simple uint64",
			input: uint64(18446744073709551615),
		},
		{
			name:  "time.Time",
			input: time.Now(),
		},
		{
			name:  "simple map",
			input: map[string]any{"str": "value", "uint": uint64(123), "int": 42},
		},
		{
			name: "complex nested structure",
			input: map[string]any{
				"id":   uint64(18446744073709551615),
				"name": "test",
				"metadata": map[string]any{
					"created": time.Now(),
					"tags":    []string{"test", "benchmark"},
					"counts":  []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
					"nested": map[string]any{
						"deep": map[string]any{
							"deeper": map[string]any{
								"deepest": uint64(9876543210),
							},
						},
					},
				},
				"items": []any{
					uint64(1),
					"test",
					map[string]any{
						"nestedUint": uint64(12345),
						"nestedTime": time.Now(),
					},
				},
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()
			for b.Loop() {
				_ = processor.ProcessValue(bm.input)
			}
		})
	}
}
