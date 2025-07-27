package session_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValueProcessor_ProcessValue(t *testing.T) {
	t.Parallel()

	processor := session.NewValueProcessor()

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
				"value":                          "18446744073709551615",
				session.TypeMetadataPrefix + "0": session.NumericTypeMeta,
			},
		},
		{
			name:  "small uint64 value",
			input: uint64(42),
			expected: map[string]any{
				"value":                          "42",
				session.TypeMetadataPrefix + "0": session.NumericTypeMeta,
			},
		},
		{
			name:     "time.Time value",
			input:    now,
			expected: now.Format(time.RFC3339Nano),
		},
		{
			name:     "zero time.Time value",
			input:    time.Time{},
			expected: time.Time{}.Format(time.RFC3339Nano),
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
			name:     "empty slice",
			input:    []uint64{},
			expected: []any{},
		},
		{
			name:  "slice of uint64",
			input: []uint64{1, 2, 18446744073709551615},
			expected: []any{
				map[string]any{"value": "1", session.TypeMetadataPrefix + "0": session.NumericTypeMeta},
				map[string]any{"value": "2", session.TypeMetadataPrefix + "0": session.NumericTypeMeta},
				map[string]any{"value": "18446744073709551615", session.TypeMetadataPrefix + "0": session.NumericTypeMeta},
			},
		},
		{
			name:  "slice of mixed types",
			input: []any{"string", uint64(123), 42, true},
			expected: []any{
				"string",
				map[string]any{"value": "123", session.TypeMetadataPrefix + "0": session.NumericTypeMeta},
				42,
				true,
			},
		},
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: map[string]any{},
		},
		{
			name:  "map with string keys",
			input: map[string]any{"str": "value", "uint": uint64(123), "int": 42},
			expected: map[string]any{
				"str":  "value",
				"uint": map[string]any{"value": "123", session.TypeMetadataPrefix + "0": session.NumericTypeMeta},
				"int":  42,
			},
		},
		{
			name:  "nested map and slice",
			input: map[string]any{"items": []any{uint64(1), "test", map[string]any{"nestedUint": uint64(12345)}}},
			expected: map[string]any{
				"items": []any{
					map[string]any{"value": "1", session.TypeMetadataPrefix + "0": session.NumericTypeMeta},
					"test",
					map[string]any{
						"nestedUint": map[string]any{"value": "12345", session.TypeMetadataPrefix + "0": session.NumericTypeMeta},
					},
				},
			},
		},
		{
			name:     "nil pointer",
			input:    (*string)(nil),
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := processor.ProcessValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}

	t.Run("struct with fields", func(t *testing.T) {
		t.Parallel()

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
		assert.Equal(t, session.NumericTypeMeta, idMap[session.TypeMetadataPrefix+"0"])

		// Check other fields
		assert.Equal(t, "Test", resultMap["Name"])
		assert.Equal(t, now.Format(time.RFC3339Nano), resultMap["CreatedAt"])
	})

	t.Run("struct with zero-valued fields", func(t *testing.T) {
		t.Parallel()

		type ZeroTestStruct struct {
			ID        uint64
			Name      string
			Empty     string
			ZeroInt   int
			CreatedAt time.Time
			Tags      []string
			Metadata  map[string]string
		}

		input := ZeroTestStruct{
			ID:        18446744073709551615,
			Name:      "Test",
			Empty:     "", // Zero value for string
			ZeroInt:   0,  // Zero value for int
			CreatedAt: now,
			Tags:      nil, // Zero value for slice
			Metadata:  nil, // Zero value for map
		}

		result := processor.ProcessValue(input)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map")

		// Only non-zero fields should be present
		assert.Contains(t, resultMap, "ID")
		assert.Contains(t, resultMap, "Name")
		assert.Contains(t, resultMap, "CreatedAt")

		// Zero-valued fields should be skipped
		assert.NotContains(t, resultMap, "Empty")
		assert.NotContains(t, resultMap, "ZeroInt")
		assert.NotContains(t, resultMap, "Tags")
		assert.NotContains(t, resultMap, "Metadata")
	})

	t.Run("nested struct with zero values", func(t *testing.T) {
		t.Parallel()

		type Address struct {
			Street  string
			City    string
			Country string
			ZipCode string
		}

		type Person struct {
			Name    string
			Age     int
			Address Address
			Contact *Address
		}

		// Case 1: Some fields populated, others zero
		t.Run("mixed values", func(t *testing.T) {
			t.Parallel()

			input := Person{
				Name: "Test Person",
				Age:  30,
				Address: Address{
					Street:  "123 Main St",
					Country: "USA",
					// City and ZipCode are zero-valued
				},
				// Contact is nil
			}

			result := processor.ProcessValue(input)
			resultMap, ok := result.(map[string]any)
			require.True(t, ok, "Expected result to be a map")

			// Check top-level fields
			assert.Contains(t, resultMap, "Name")
			assert.Contains(t, resultMap, "Age")
			assert.Contains(t, resultMap, "Address")
			assert.NotContains(t, resultMap, "Contact")

			// Check nested Address fields
			addressMap, ok := resultMap["Address"].(map[string]any)
			require.True(t, ok, "Expected Address to be a map")
			assert.Contains(t, addressMap, "Street")
			assert.Contains(t, addressMap, "Country")
			assert.NotContains(t, addressMap, "City")
			assert.NotContains(t, addressMap, "ZipCode")
		})

		// Case 2: All fields populated
		t.Run("all fields populated", func(t *testing.T) {
			t.Parallel()

			contactInfo := &Address{
				Street:  "456 Work St",
				City:    "Work City",
				Country: "Canada",
				ZipCode: "W1234",
			}

			input := Person{
				Name: "Test Person",
				Age:  30,
				Address: Address{
					Street:  "123 Main St",
					City:    "Home City",
					Country: "USA",
					ZipCode: "H5678",
				},
				Contact: contactInfo,
			}

			result := processor.ProcessValue(input)
			resultMap, ok := result.(map[string]any)
			require.True(t, ok, "Expected result to be a map")

			// All fields should be present
			assert.Contains(t, resultMap, "Name")
			assert.Contains(t, resultMap, "Age")
			assert.Contains(t, resultMap, "Address")
			assert.Contains(t, resultMap, "Contact")

			// Check nested Address fields
			addressMap, ok := resultMap["Address"].(map[string]any)
			require.True(t, ok, "Expected Address to be a map")
			assert.Contains(t, addressMap, "Street")
			assert.Contains(t, addressMap, "City")
			assert.Contains(t, addressMap, "Country")
			assert.Contains(t, addressMap, "ZipCode")

			// Check nested Contact fields
			contactMap, ok := resultMap["Contact"].(map[string]any)
			require.True(t, ok, "Expected Contact to be a map")
			assert.Contains(t, contactMap, "Street")
			assert.Contains(t, contactMap, "City")
			assert.Contains(t, contactMap, "Country")
			assert.Contains(t, contactMap, "ZipCode")
		})
	})

	t.Run("pointer to uint64", func(t *testing.T) {
		t.Parallel()

		val := uint64(18446744073709551615)
		ptr := &val
		result := processor.ProcessValue(ptr)

		// Check result is a map with metadata
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map with metadata")
		assert.Equal(t, "18446744073709551615", resultMap["value"])
		assert.Equal(t, session.NumericTypeMeta, resultMap[session.TypeMetadataPrefix+"0"])
	})

	t.Run("mock User structure", func(t *testing.T) {
		t.Parallel()

		type MockUser struct {
			ID             uint64
			Name           string
			DisplayName    string
			Description    string
			LastViewed     time.Time
			IsBanned       bool
			ThumbnailURL   string
			Confidence     float64
			Reasons        map[string]string
			Friends        []uint64
			Groups         []uint64
			EmptySlice     []string
			ZeroConfidence float64
			EmptyMap       map[string]int
		}

		// Create test user with mix of zero and non-zero values
		input := MockUser{
			ID:             12345,
			Name:           "TestUser",
			DisplayName:    "Test User",
			Description:    "", // Zero-valued - should be skipped
			LastViewed:     now,
			IsBanned:       false, // Zero-valued - should be skipped
			ThumbnailURL:   "https://example.com/avatar.png",
			Confidence:     0.95,
			Reasons:        map[string]string{"reason1": "test reason"},
			Friends:        []uint64{1, 2, 3},
			Groups:         []uint64{}, // Empty slice - the current implementation preserves this
			EmptySlice:     nil,        // Nil slice - should be skipped
			ZeroConfidence: 0.0,        // Zero float - should be skipped
			EmptyMap:       nil,        // Nil map - should be skipped
		}

		result := processor.ProcessValue(input)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map")

		// Fields that should be present
		assert.Contains(t, resultMap, "ID")
		assert.Contains(t, resultMap, "Name")
		assert.Contains(t, resultMap, "DisplayName")
		assert.Contains(t, resultMap, "LastViewed")
		assert.Contains(t, resultMap, "ThumbnailURL")
		assert.Contains(t, resultMap, "Confidence")
		assert.Contains(t, resultMap, "Reasons")
		assert.Contains(t, resultMap, "Friends")
		assert.Contains(t, resultMap, "Groups")

		// Fields that should be skipped
		assert.NotContains(t, resultMap, "Description")
		assert.NotContains(t, resultMap, "IsBanned")
		assert.NotContains(t, resultMap, "EmptySlice") // Nil slices are skipped
		assert.NotContains(t, resultMap, "ZeroConfidence")
		assert.NotContains(t, resultMap, "EmptyMap")

		// Verify Friends is properly converted
		friends, ok := resultMap["Friends"].([]any)
		require.True(t, ok, "Expected Friends to be an array")
		assert.Len(t, friends, 3)

		// Verify Groups is an empty array
		groups, ok := resultMap["Groups"].([]any)
		require.True(t, ok, "Expected Groups to be an array")
		assert.Empty(t, groups)
	})
}

func TestValueProcessor_EmbeddedStructHandling(t *testing.T) {
	t.Parallel()

	processor := session.NewValueProcessor()

	type Friend struct {
		ID uint64 `json:"id"`
	}

	type ExtendedFriend struct {
		Friend

		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	}

	t.Run("EmbeddedStructFlattening", func(t *testing.T) {
		t.Parallel()
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
		require.Len(t, result, 2, "Expected 2 friends")
		require.Contains(t, result[0], "id", "Expected id field to be at top level")

		// Check that ID is now a map with type metadata
		idValue, ok := result[0]["id"].(map[string]any)
		require.True(t, ok, "Expected id to be a map with metadata")
		require.Equal(t, "2892328990", idValue["value"], "Expected ID value to be preserved")
		require.Equal(t, session.NumericTypeMeta, idValue[session.TypeMetadataPrefix+"0"], "Expected numeric type metadata")

		require.NotContains(t, result[0], "Friend", "Friend struct should be flattened")
	})

	t.Run("embedded struct with zero values", func(t *testing.T) {
		t.Parallel()

		type BaseInfo struct {
			ID      uint64    `json:"id"`
			Created time.Time `json:"created"`
			Updated time.Time `json:"updated"` // Will be zero
		}

		type User struct {
			BaseInfo

			Username string `json:"username"`
			Email    string `json:"email"` // Will be zero
		}

		input := User{
			BaseInfo: BaseInfo{
				ID:      12345,
				Created: time.Now(),
				// Updated is zero
			},
			Username: "testuser",
			// Email is zero
		}

		result := processor.ProcessValue(input)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map")

		// Fields that should be present
		assert.Contains(t, resultMap, "id")
		assert.Contains(t, resultMap, "created")
		assert.Contains(t, resultMap, "username")

		// Fields that should be skipped
		assert.NotContains(t, resultMap, "updated")
		assert.NotContains(t, resultMap, "email")
	})

	t.Run("explicit JSON tag on embedded struct", func(t *testing.T) {
		t.Parallel()

		type Base struct {
			ID uint64 `json:"id"`
		}

		type Tagged struct {
			Base `json:"base"` // Explicit JSON tag

			Name string `json:"name"`
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
		require.Equal(t, session.NumericTypeMeta, idValue[session.TypeMetadataPrefix+"0"], "Expected numeric type metadata")

		assert.Equal(t, "TestTagged", resultMap["name"])
	})
}

func TestValueProcessor_EnumMapHandling(t *testing.T) {
	t.Parallel()

	processor := session.NewValueProcessor()

	type ReasonType int

	const (
		reasonTypeUser ReasonType = iota
		reasonTypeFriend
		reasonTypeOutfit
		reasonTypeGroup
		reasonTypeMember
		reasonTypeCustom
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
		t.Parallel()
		// Create test data with enum map keys
		original := map[ReasonType]*Reason{
			reasonTypeUser: {
				Message:    "User has inappropriate content",
				Confidence: 0.85,
				Evidence:   []string{"profile text", "username"},
			},
			reasonTypeFriend: {
				Message:    "User has flagged friends",
				Confidence: 0.75,
				Evidence:   []string{"friend1", "friend2"},
			},
			reasonTypeOutfit: nil,
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
		require.InEpsilon(t, 0.85, reason0["confidence"], 0.01)
	})

	t.Run("ComplexObjectWithEmbeddedAndMaps", func(t *testing.T) {
		t.Parallel()
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
			reasonTypeUser: {
				Message:    "User has inappropriate content",
				Confidence: 0.85,
				Evidence:   []string{"profile text", "username"},
			},
			reasonTypeFriend: {
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
		require.Equal(t, session.NumericTypeMeta, idValue[session.TypeMetadataPrefix+"0"], "Expected numeric type metadata")

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
		require.InEpsilon(t, 0.85, reasonObj["confidence"], 0.01)

		// Verify friends are properly processed
		friendsSlice, ok := userMap["friends"].([]any)
		require.True(t, ok, "Expected friends to be []any")
		require.Len(t, friendsSlice, 2, "Expected 2 friends")

		// Verify first friend has flattened structure
		firstFriend, ok := friendsSlice[0].(map[string]any)
		require.True(t, ok, "Expected friend to be map[string]any")
		require.Contains(t, firstFriend, "id", "Expected id field to be at top level")

		// Check that ID is now a map with type metadata
		friendIDValue, ok := firstFriend["id"].(map[string]any)
		require.True(t, ok, "Expected id to be a map with metadata")
		require.Equal(t, "2892328990", friendIDValue["value"], "Expected ID value to be preserved")
		require.Equal(t, session.NumericTypeMeta, friendIDValue[session.TypeMetadataPrefix+"0"], "Expected numeric type metadata")

		require.NotContains(t, firstFriend, "Friend", "Friend struct should be flattened")
	})
}

func TestValueProcessor_JSONTagHandling(t *testing.T) {
	t.Parallel()

	processor := session.NewValueProcessor()

	t.Run("json tag name", func(t *testing.T) {
		t.Parallel()

		type TestStruct struct {
			RegularField string `json:"regularField"`
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

		assert.Equal(t, "regular", resultMap["regularField"])
		assert.Equal(t, "renamed", resultMap["renamed"])
		assert.Equal(t, "default", resultMap["DefaultField"])
		assert.NotContains(t, resultMap, "OmittedField")
		assert.NotContains(t, resultMap, "-")
	})

	t.Run("json tag options", func(t *testing.T) {
		t.Parallel()

		type TestStruct struct {
			RequiredField       string `json:"requiredField"`
			OmitemptyField      string `json:"omitemptyField,omitempty"`
			EmptyOmitemptyField string `json:"emptyOmitempty,omitempty"`
			NonEmptyWithTag     string `json:"nonEmpty,omitempty"`
		}

		input := TestStruct{
			RequiredField:       "required",
			OmitemptyField:      "not empty",
			EmptyOmitemptyField: "",
			NonEmptyWithTag:     "present",
		}

		result := processor.ProcessValue(input)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok, "Expected result to be a map")

		// Fields with values should be present
		assert.Equal(t, "required", resultMap["requiredField"])
		assert.Equal(t, "not empty", resultMap["omitemptyField"])
		assert.Equal(t, "present", resultMap["nonEmpty"])

		// Empty string fields are skipped regardless of tag options
		assert.NotContains(t, resultMap, "emptyOmitempty", "Empty strings are skipped by our implementation")
	})
}

func BenchmarkValueProcessor_ProcessValue(b *testing.B) {
	processor := session.NewValueProcessor()

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
