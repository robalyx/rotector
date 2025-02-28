package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessValue(t *testing.T) {
	// Current time for testing time values
	now := time.Now().UTC().Truncate(time.Microsecond)

	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: nil,
		},
		{
			name:     "uint64 value",
			input:    uint64(18446744073709551615),
			expected: "18446744073709551615",
		},
		{
			name:     "small uint64 value",
			input:    uint64(42),
			expected: "42",
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
			name:     "slice of uint64",
			input:    []uint64{1, 2, 18446744073709551615},
			expected: []string{"1", "2", "18446744073709551615"},
		},
		{
			name:     "slice of mixed types",
			input:    []interface{}{"string", uint64(123), 42, true},
			expected: []interface{}{"string", "123", 42, true},
		},
		{
			name:     "map with string keys",
			input:    map[string]interface{}{"str": "value", "uint": uint64(123), "int": 42},
			expected: map[string]interface{}{"str": "value", "uint": "123", "int": 42},
		},
		{
			name:     "nested map and slice",
			input:    map[string]interface{}{"items": []interface{}{uint64(1), "test", map[string]interface{}{"nestedUint": uint64(12345)}}},
			expected: map[string]interface{}{"items": []interface{}{"1", "test", map[string]interface{}{"nestedUint": "12345"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}

	// Test struct handling with an example struct
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

		result := processValue(input)
		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "Expected result to be a map")
		assert.Equal(t, "18446744073709551615", resultMap["ID"])
		assert.Equal(t, "Test", resultMap["Name"])
		assert.Equal(t, now.Format(time.RFC3339Nano), resultMap["CreatedAt"])
	})

	// Test pointer type
	t.Run("pointer to uint64", func(t *testing.T) {
		val := uint64(18446744073709551615)
		ptr := &val
		result := processValue(ptr)
		assert.Equal(t, "18446744073709551615", result)
	})
}

func TestPreserveNumericPrecision(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: nil,
		},
		{
			name:     "string representing uint64",
			input:    "18446744073709551615",
			expected: uint64(18446744073709551615),
		},
		{
			name:     "string not representing number",
			input:    "test string",
			expected: "test string",
		},
		{
			name:     "json.Number representing uint64",
			input:    json.Number("18446744073709551615"),
			expected: uint64(18446744073709551615),
		},
		{
			name:     "json.Number representing negative int",
			input:    json.Number("-123"),
			expected: json.Number("-123").String(),
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
			name: "map with mixed values",
			input: map[string]interface{}{
				"uint_str":  "12345",
				"uint_json": json.Number("9876543210"),
				"float":     json.Number("123.45"),
				"regular":   "string",
			},
			expected: map[string]interface{}{
				"uint_str":  uint64(12345),
				"uint_json": uint64(9876543210),
				"float":     123.45,
				"regular":   "string",
			},
		},
		{
			name: "slice with mixed values",
			input: []interface{}{
				"12345",
				json.Number("9876543210"),
				json.Number("123.45"),
				"string",
			},
			expected: []interface{}{
				uint64(12345),
				uint64(9876543210),
				123.45,
				"string",
			},
		},
		{
			name: "nested structures",
			input: map[string]interface{}{
				"items": []interface{}{
					"123",
					map[string]interface{}{
						"nested": json.Number("18446744073709551615"),
					},
				},
			},
			expected: map[string]interface{}{
				"items": []interface{}{
					uint64(123),
					map[string]interface{}{
						"nested": uint64(18446744073709551615),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preserveNumericPrecision(tt.input)
			if fmt.Sprintf("%v", result) != fmt.Sprintf("%v", tt.expected) {
				t.Errorf("preserveNumericPrecision(%v) = %v, want %v (types: %T vs %T)",
					tt.input, result, tt.expected, result, tt.expected)
			}
		})
	}

	// Test a large value that would lose precision if stored as float64
	t.Run("large uint64 value that would lose precision", func(t *testing.T) {
		input := json.Number("18446744073709551615") // max uint64
		result := preserveNumericPrecision(input)
		resultUint, ok := result.(uint64)
		require.True(t, ok, "Expected result to be uint64, got %T", result)
		assert.Equal(t, uint64(18446744073709551615), resultUint)

		// Verify this would lose precision if converted to float64
		asFloat := float64(18446744073709551615)
		assert.NotEqual(t, uint64(18446744073709551615), uint64(asFloat),
			"Converting to float64 should lose precision, but didn't")
	})
}

func BenchmarkProcessValue(b *testing.B) {
	benchmarks := []struct {
		name  string
		input interface{}
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
			input: map[string]interface{}{"str": "value", "uint": uint64(123), "int": 42},
		},
		{
			name: "complex nested structure",
			input: map[string]interface{}{
				"id":   uint64(18446744073709551615),
				"name": "test",
				"metadata": map[string]interface{}{
					"created": time.Now(),
					"tags":    []string{"test", "benchmark"},
					"counts":  []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
					"nested": map[string]interface{}{
						"deep": map[string]interface{}{
							"deeper": map[string]interface{}{
								"deepest": uint64(9876543210),
							},
						},
					},
				},
				"items": []interface{}{
					uint64(1),
					"test",
					map[string]interface{}{
						"nestedUint": uint64(12345),
						"nestedTime": time.Now(),
					},
				},
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Reset the timer for the actual benchmark
			b.ResetTimer()
			for b.Loop() {
				_ = processValue(bm.input)
			}
		})
	}
}

func BenchmarkPreserveNumericPrecision(b *testing.B) {
	benchmarks := []struct {
		name  string
		input interface{}
	}{
		{
			name:  "simple string uint64",
			input: "18446744073709551615",
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
			name: "simple map",
			input: map[string]interface{}{
				"uint_str":  "12345",
				"uint_json": json.Number("9876543210"),
				"float":     json.Number("123.45"),
				"regular":   "string",
			},
		},
		{
			name: "complex nested structure",
			input: map[string]interface{}{
				"id":   json.Number("18446744073709551615"),
				"name": "test",
				"metadata": map[string]interface{}{
					"created": "2023-10-15T12:00:00Z",
					"tags":    []string{"test", "benchmark"},
					"counts": []interface{}{
						json.Number("1"),
						json.Number("2"),
						json.Number("3"),
						json.Number("4"),
						json.Number("5"),
					},
					"nested": map[string]interface{}{
						"deep": map[string]interface{}{
							"deeper": map[string]interface{}{
								"deepest": json.Number("9876543210"),
							},
						},
					},
				},
				"items": []interface{}{
					"123",
					"test",
					map[string]interface{}{
						"nestedUint":   json.Number("12345"),
						"nestedBigInt": json.Number("18446744073709551615"),
					},
				},
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Reset the timer for the actual benchmark
			b.ResetTimer()
			for b.Loop() {
				_ = preserveNumericPrecision(bm.input)
			}
		})
	}
}

// BenchmarkDoubleMarshaling simulates the full double marshal/unmarshal process
// that happens in getInterface to handle uint64 values
func BenchmarkDoubleMarshaling(b *testing.B) {
	benchmarks := []struct {
		name  string
		input interface{}
	}{
		{
			name:  "simple map",
			input: map[string]interface{}{"id": json.Number("12345"), "name": "test"},
		},
		{
			name: "complex nested structure",
			input: map[string]interface{}{
				"id":   json.Number("18446744073709551615"),
				"name": "test",
				"metadata": map[string]interface{}{
					"counts": []interface{}{
						json.Number("1"),
						json.Number("2"),
						json.Number("3"),
					},
					"nested": map[string]interface{}{
						"deepest": json.Number("9876543210"),
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

				var rawData interface{}
				decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
				decoder.UseNumber()
				_ = decoder.Decode(&rawData)

				processedData := preserveNumericPrecision(rawData)

				processedBytes, _ := json.Marshal(processedData)

				var result map[string]interface{}
				_ = json.Unmarshal(processedBytes, &result)
			}
		})
	}
}
