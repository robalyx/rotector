package session

import (
	"encoding/json"
	"strconv"
	"strings"
)

// NumericProcessor encapsulates methods for handling numeric precision in data structures.
// This helps maintain precision for large integer values like uint64 during serialization.
type NumericProcessor struct{}

// NewNumericProcessor creates a new instance of the numeric processor.
func NewNumericProcessor() *NumericProcessor {
	return &NumericProcessor{}
}

// NumericMetadata tracks information about numeric conversions to help
// determine whether values should be converted to integers or remain as strings.
type NumericMetadata struct {
	// IsNumeric indicates if the string represents a numeric value
	IsNumeric bool
	// IsUint64 indicates if the string can be parsed as a uint64
	IsUint64 bool
	// IsInt64 indicates if the string can be parsed as an int64
	IsInt64 bool
	// IsFloat indicates if the string should remain as a float
	IsFloat bool
}

// AnalyzeNumeric examines a string to determine its numeric properties.
func (p *NumericProcessor) AnalyzeNumeric(s string) NumericMetadata {
	meta := NumericMetadata{}

	// Try parsing as uint64 first (most common case for IDs)
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		meta.IsNumeric = true
		meta.IsUint64 = true
		// Check if this is actually a small value that could be an enum
		if u < 1000 {
			// Small values might be enums, so we'll mark them for conversion
			meta.IsInt64 = true
		}

		return meta
	}

	// Try parsing as int64 next
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		meta.IsNumeric = true
		meta.IsInt64 = true

		return meta
	}

	// Check if it's a float
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		meta.IsNumeric = true
		meta.IsFloat = true
	}

	return meta
}

// PreserveNumericPrecision recursively processes a data structure and converts
// json.Number and numeric strings to uint64 where appropriate, maintaining
// precision for large integer values.
func (p *NumericProcessor) PreserveNumericPrecision(data any) any {
	if data == nil {
		return nil
	}

	switch v := data.(type) {
	case string:
		return p.processString(v, false)
	case json.Number:
		return p.processJSONNumber(v)
	case float64:
		return p.processFloat64(v)
	case []any:
		return p.processSlice(v)
	case map[string]any:
		return p.processMap(v)
	default:
		return v
	}
}

// processString handles conversion of string values to appropriate numeric types.
// Only converts strings to numbers when type metadata explicitly indicates to do so.
func (p *NumericProcessor) processString(v string, hasTypeMetadata bool) any {
	// Only convert to numeric types if we have explicit type metadata
	if !hasTypeMetadata {
		return v
	}

	// When we have type metadata, attempt conversion
	meta := p.AnalyzeNumeric(v)
	switch {
	case meta.IsUint64:
		if u, err := strconv.ParseUint(v, 10, 64); err == nil {
			return u
		}
	case meta.IsInt64:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}

	// Keep as string if conversion fails
	return v
}

// processJSONNumber handles conversion of json.Number values to appropriate types.
// For backward compatibility, json.Number is treated as having implicit type metadata.
func (p *NumericProcessor) processJSONNumber(v json.Number) any {
	// Convert json.Number to string first
	s := v.String()
	if s == "" {
		return s
	}

	// Process json.Number with type information (for backward compatibility)
	// since json.Number is explicitly marked as numeric in the JSON
	meta := p.AnalyzeNumeric(s)

	// Convert based on metadata
	switch {
	case meta.IsUint64:
		if u, err := strconv.ParseUint(s, 10, 64); err == nil {
			return u
		}
	case meta.IsInt64:
		if i, err := v.Int64(); err == nil {
			return i
		}
	case meta.IsFloat:
		if f, err := v.Float64(); err == nil {
			return f
		}
	}

	// Fall back to string if we can't determine numeric type
	return s
}

// processFloat64 handles conversion of float64 values to uint64 when appropriate.
func (p *NumericProcessor) processFloat64(v float64) any {
	// If the float is actually an integer and non-negative, convert to uint64
	if v == float64(uint64(v)) && v >= 0 {
		return uint64(v)
	}

	return v
}

// processSlice recursively processes each element in a slice.
func (p *NumericProcessor) processSlice(v []any) any {
	// Process each item in slice
	result := make([]any, len(v))
	for i, item := range v {
		// Check if this item has type metadata
		if itemMap, ok := item.(map[string]any); ok {
			if val, hasVal := itemMap["value"]; hasVal {
				if typeMeta, hasType := itemMap[TypeMetadataPrefix+"0"]; hasType && typeMeta == NumericTypeMeta {
					if strVal, isStr := val.(string); isStr {
						// This is a string with numeric type metadata
						result[i] = p.processString(strVal, true)
						continue
					}
				}
			}
		}

		// Standard processing for items without metadata
		result[i] = p.PreserveNumericPrecision(item)
	}

	return result
}

// processMap recursively processes all values in a map and handles special case for numeric keys.
func (p *NumericProcessor) processMap(v map[string]any) any {
	// Check if this is a wrapped numeric value
	if val, hasVal := v["value"]; hasVal {
		if typeMeta, hasType := v[TypeMetadataPrefix+"0"]; hasType && typeMeta == NumericTypeMeta {
			if strVal, isStr := val.(string); isStr {
				// This is a string with numeric type metadata
				return p.processString(strVal, true)
			}
		}
	}

	// Extract type metadata for keys
	keyTypeMap := make(map[string]bool)

	for k, v := range v {
		if strings.HasPrefix(k, TypeMetadataPrefix) && v == NumericTypeMeta {
			// Record that this key has numeric type metadata
			keyTypeMap[strings.TrimPrefix(k, TypeMetadataPrefix)] = true
		}
	}

	// Process all values in the map
	result := make(map[string]any)

	for k, val := range v {
		// Skip type metadata entries
		if strings.HasPrefix(k, TypeMetadataPrefix) {
			continue
		}

		// Process the value
		result[k] = p.PreserveNumericPrecision(val)
	}

	// Only convert keys if we have explicit type metadata
	if len(keyTypeMap) > 0 {
		intMap := make(map[int]any)
		allKeysConverted := true

		// Try to convert all keys that have metadata
		for k, val := range result {
			if _, hasTypeMeta := keyTypeMap[k]; hasTypeMeta {
				// This key has metadata, try to convert it
				if idx, err := strconv.Atoi(k); err == nil {
					intMap[idx] = val
				} else {
					// Can't convert despite metadata
					allKeysConverted = false
					break
				}
			} else {
				// This key doesn't have metadata, don't convert
				allKeysConverted = false
				break
			}
		}

		// Only return the int map if all keys with metadata were converted
		// and all keys in the map had metadata
		if allKeysConverted && len(intMap) > 0 && len(intMap) == len(result) {
			return intMap
		}
	}

	// Otherwise return the original string-keyed map
	return result
}
