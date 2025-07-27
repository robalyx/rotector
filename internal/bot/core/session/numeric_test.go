package session_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreserveNumericPrecision(t *testing.T) {
	t.Parallel()

	processor := session.NewNumericProcessor()

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
			name:     "string representing uint64 without metadata",
			input:    "18446744073709551615",
			expected: "18446744073709551615",
		},
		{
			name:     "string not representing number",
			input:    "test string",
			expected: "test string",
		},
		{
			name:     "string representing negative number without metadata",
			input:    "-123",
			expected: "-123",
		},
		{
			name:     "string representing floating point number without metadata",
			input:    "123.45",
			expected: "123.45",
		},
		{
			name:     "string representing number larger than uint64 max without metadata",
			input:    "18446744073709551616", // uint64 max + 1
			expected: "18446744073709551616",
		},
		{
			name:     "json.Number representing uint64",
			input:    json.Number("18446744073709551615"),
			expected: uint64(18446744073709551615),
		},
		{
			name:     "json.Number representing negative int",
			input:    json.Number("-123"),
			expected: int64(-123),
		},
		{
			name:     "json.Number representing float",
			input:    json.Number("123.45"),
			expected: 123.45,
		},
		{
			name:     "float64 that looks like uint",
			input:    float64(42),
			expected: uint64(42),
		},
		{
			name:     "float64 with decimal",
			input:    float64(42.5),
			expected: float64(42.5),
		},
		{
			name: "map with string values",
			input: map[string]any{
				"uint_str":  "12345",
				"uint_json": json.Number("9876543210"),
				"float":     json.Number("123.45"),
				"regular":   "string",
			},
			expected: map[string]any{
				"uint_str":  "12345",
				"uint_json": uint64(9876543210),
				"float":     123.45,
				"regular":   "string",
			},
		},
		{
			name: "slice with mixed values",
			input: []any{
				"12345",
				json.Number("9876543210"),
				json.Number("123.45"),
				"string",
			},
			expected: []any{
				"12345",            // No conversion without metadata
				uint64(9876543210), // json.Number is treated as having implicit type info
				123.45,
				"string",
			},
		},
		{
			name: "nested structures",
			input: map[string]any{
				"items": []any{
					"123",
					map[string]any{
						"nested": json.Number("18446744073709551615"),
					},
				},
			},
			expected: map[string]any{
				"items": []any{
					"123",
					map[string]any{
						"nested": uint64(18446744073709551615),
					},
				},
			},
		},
		{
			name: "string with type metadata",
			input: map[string]any{
				"value":                          "12345",
				session.TypeMetadataPrefix + "0": session.NumericTypeMeta,
			},
			expected: uint64(12345), // Convert with type metadata
		},
		{
			name: "map with numeric keys with metadata",
			input: map[string]any{
				"1":                              "value1",
				"2":                              "value2",
				session.TypeMetadataPrefix + "1": session.NumericTypeMeta,
				session.TypeMetadataPrefix + "2": session.NumericTypeMeta,
			},
			expected: map[int]any{
				1: "value1",
				2: "value2",
			},
		},
		{
			name: "map with mixed key types",
			input: map[string]any{
				"1":                              "value1",
				"key":                            "value2",
				session.TypeMetadataPrefix + "1": session.NumericTypeMeta,
			},
			expected: map[string]any{
				"1":   "value1", // Not converted (original map returned because not all keys have metadata)
				"key": "value2",
			},
		},
		{
			name: "slice with metadata objects",
			input: []any{
				map[string]any{
					"value":                          "12345",
					session.TypeMetadataPrefix + "0": session.NumericTypeMeta,
				},
				"67890",
				json.Number("123"),
			},
			expected: []any{
				uint64(12345),
				"67890",
				uint64(123),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := processor.PreserveNumericPrecision(tt.input)
			assert.Equal(t, tt.expected, result,
				"processor.PreserveNumericPrecision(%v) = %v, want %v (types: %T vs %T)",
				tt.input, result, tt.expected, result, tt.expected)
		})
	}

	// Test a large value that would lose precision if stored as float64
	t.Run("large uint64 value that would lose precision", func(t *testing.T) {
		input := json.Number("18446744073709551615") // max uint64
		result := processor.PreserveNumericPrecision(input)
		resultUint, ok := result.(uint64)
		require.True(t, ok, "Expected result to be uint64, got %T", result)
		assert.Equal(t, uint64(18446744073709551615), resultUint)

		// Verify this would lose precision if converted to float64
		asFloat := float64(18446744073709551615)
		assert.NotEqual(t, uint64(18446744073709551615), uint64(asFloat),
			"Converting to float64 should lose precision, but didn't")
	})
}

func TestAnalyzeNumeric(t *testing.T) {
	t.Parallel()

	processor := session.NewNumericProcessor()

	tests := []struct {
		input     string
		isNumeric bool
		isUint64  bool
		isInt64   bool
		isFloat   bool
	}{
		{"123", true, true, true, false},
		{"0", true, true, true, false},
		{"-123", true, false, true, false},
		{"18446744073709551615", true, true, false, false}, // uint64 max
		{"123.456", true, false, false, true},
		{"not a number", false, false, false, false},
		{"123abc", false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			meta := processor.AnalyzeNumeric(tt.input)
			assert.Equal(t, tt.isNumeric, meta.IsNumeric, "IsNumeric")
			assert.Equal(t, tt.isUint64, meta.IsUint64, "IsUint64")
			assert.Equal(t, tt.isInt64, meta.IsInt64, "IsInt64")
			assert.Equal(t, tt.isFloat, meta.IsFloat, "IsFloat")
		})
	}
}

func TestTypeMetadata(t *testing.T) {
	t.Parallel()

	processor := session.NewNumericProcessor()
	valueProcessor := session.NewValueProcessor()

	t.Run("ValueProcessor adds metadata for uint64", func(t *testing.T) {
		t.Parallel()

		original := uint64(12345)
		processed := valueProcessor.ProcessValue(original)

		// Check that we got a map with value and type metadata
		processedMap, ok := processed.(map[string]any)
		require.True(t, ok, "Expected processed to be a map, got %T", processed)

		value, hasValue := processedMap["value"]
		require.True(t, hasValue, "Expected 'value' key in processed map")
		assert.Equal(t, "12345", value)

		typeMeta, hasTypeMeta := processedMap[session.TypeMetadataPrefix+"0"]
		require.True(t, hasTypeMeta, "Expected type metadata key in processed map")
		assert.Equal(t, session.NumericTypeMeta, typeMeta)
	})

	t.Run("NumericProcessor respects type metadata", func(t *testing.T) {
		t.Parallel()
		// First convert a uint64 to a string with metadata
		original := uint64(12345)
		processed := valueProcessor.ProcessValue(original)

		// Then process it back
		recovered := processor.PreserveNumericPrecision(processed)

		// Should be converted back to uint64
		assert.Equal(t, original, recovered)
		assert.IsType(t, uint64(0), recovered)
	})

	t.Run("NumericProcessor without metadata", func(t *testing.T) {
		t.Parallel()
		// Create a string value without metadata
		original := "12345"

		// Process it
		processed := processor.PreserveNumericPrecision(original)

		// Should remain as string (no conversion without metadata)
		assert.Equal(t, original, processed)
		assert.IsType(t, "", processed)
	})

	t.Run("End-to-end conversion", func(t *testing.T) {
		t.Parallel()
		// Original complex structure with uint64 values
		original := map[string]any{
			"id": uint64(18446744073709551615),
			"nested": map[string]any{
				"id": uint64(9876543210),
			},
			"items": []uint64{1, 2, 3},
		}

		// Process with ValueProcessor (simulate storing in Redis)
		processed := valueProcessor.ProcessValue(original)

		// Convert to JSON and back (simulate the double marshal/unmarshal in Session.getInterface)
		jsonBytes, err := json.Marshal(processed)
		require.NoError(t, err, "Failed to marshal processed value")

		var rawData any

		decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
		decoder.UseNumber()
		err = decoder.Decode(&rawData)
		require.NoError(t, err, "Failed to decode to raw data")

		// Process with NumericProcessor (simulate retrieving from Redis)
		recovered := processor.PreserveNumericPrecision(rawData)

		// Check that the nested values are recovered correctly
		recoveredMap, ok := recovered.(map[string]any)
		require.True(t, ok, "Expected recovered to be a map")

		idValue, hasID := recoveredMap["id"]
		require.True(t, hasID, "Expected 'id' in recovered map")
		assert.IsType(t, uint64(0), idValue)
		assert.Equal(t, uint64(18446744073709551615), idValue)

		nestedMap, hasNested := recoveredMap["nested"].(map[string]any)
		require.True(t, hasNested, "Expected 'nested' to be a map")

		nestedID, hasNestedID := nestedMap["id"]
		require.True(t, hasNestedID, "Expected 'id' in nested map")
		assert.IsType(t, uint64(0), nestedID)
		assert.Equal(t, uint64(9876543210), nestedID)
	})
}

func TestEnumMapConversion(t *testing.T) {
	t.Parallel()

	processor := session.NewNumericProcessor()
	valueProcessor := session.NewValueProcessor()

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
	processed := valueProcessor.ProcessValue(original)

	// Convert to JSON
	jsonBytes, err := json.Marshal(processed)
	require.NoError(t, err, "Failed to marshal original data")

	// Decode with json.Number preservation
	var rawData any

	decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.UseNumber()
	err = decoder.Decode(&rawData)
	require.NoError(t, err, "Failed to decode to raw data")

	// Process with NumericProcessor
	recovered := processor.PreserveNumericPrecision(rawData)

	// Verify the result is a map with int keys
	processedMap, ok := recovered.(map[int]any)
	require.True(t, ok, "Expected processed result to be map[int]any, got %T", recovered)

	// Check that we have the expected keys
	require.Contains(t, processedMap, 0, "Expected to find key for ReasonTypeUser (0)")
	require.Contains(t, processedMap, 1, "Expected to find key for ReasonTypeFriend (1)")

	// Verify the values were preserved
	reason0, ok := processedMap[0].(map[string]any)
	require.True(t, ok, "Expected reason to be map[string]any")
	require.Equal(t, "User has inappropriate content", reason0["message"])
	require.InEpsilon(t, 0.85, reason0["confidence"], 0.01)
}

func BenchmarkNumericProcessor(b *testing.B) {
	processor := session.NewNumericProcessor()
	valueProcessor := session.NewValueProcessor()

	benchmarks := []struct {
		name  string
		input any
	}{
		{
			name:  "simple string uint64 with metadata",
			input: valueProcessor.ProcessValue(uint64(18446744073709551615)),
		},
		{
			name:  "simple json.Number",
			input: json.Number("9876543210"),
		},
		{
			name:  "float64 as int",
			input: float64(42),
		},
		{
			name: "complex nested structure",
			input: map[string]any{
				"id":   valueProcessor.ProcessValue(uint64(18446744073709551615)),
				"name": "test",
				"metadata": map[string]any{
					"created": "2023-10-15T12:00:00Z",
					"tags":    []string{"test", "benchmark"},
					"counts": []any{
						valueProcessor.ProcessValue(uint64(1)),
						valueProcessor.ProcessValue(uint64(2)),
						valueProcessor.ProcessValue(uint64(3)),
					},
					"nested": map[string]any{
						"deepest": valueProcessor.ProcessValue(uint64(9876543210)),
					},
				},
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run("struct_"+bm.name, func(b *testing.B) {
			b.ResetTimer()

			for b.Loop() {
				_ = processor.PreserveNumericPrecision(bm.input)
			}
		})
	}
}

func BenchmarkDoubleMarshaling(b *testing.B) {
	processor := session.NewNumericProcessor()
	valueProcessor := session.NewValueProcessor()

	benchmarks := []struct {
		name  string
		input any
	}{
		{
			name:  "simple map",
			input: map[string]any{"id": valueProcessor.ProcessValue(uint64(12345)), "name": "test"},
		},
		{
			name: "complex nested structure",
			input: map[string]any{
				"id":   valueProcessor.ProcessValue(uint64(18446744073709551615)),
				"name": "test",
				"metadata": map[string]any{
					"counts": []any{
						valueProcessor.ProcessValue(uint64(1)),
						valueProcessor.ProcessValue(uint64(2)),
						valueProcessor.ProcessValue(uint64(3)),
					},
					"nested": map[string]any{
						"deepest": valueProcessor.ProcessValue(uint64(9876543210)),
					},
				},
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()

			for b.Loop() {
				// Simulate the double marshaling process from getInterface
				jsonBytes, _ := json.Marshal(bm.input)

				var rawData any

				decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
				decoder.UseNumber()
				_ = decoder.Decode(&rawData)

				processedData := processor.PreserveNumericPrecision(rawData)

				processedBytes, _ := json.Marshal(processedData)

				var result map[string]any

				_ = json.Unmarshal(processedBytes, &result)
			}
		})
	}
}
