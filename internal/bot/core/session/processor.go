package session

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	// TypeMetadataPrefix is used to prefix all type metadata fields.
	TypeMetadataPrefix = "__type:"
	// NumericTypeMeta is used to mark numeric fields that were converted to strings.
	NumericTypeMeta = "numeric"
)

// ValueProcessor encapsulates the logic for processing values before serialization.
// It converts special types like uint64 to string and properly handles embedded structs.
type ValueProcessor struct{}

// NewValueProcessor creates a new instance of the value processor.
func NewValueProcessor() *ValueProcessor {
	return &ValueProcessor{}
}

// ProcessValue recursively processes complex data types for JSON serialization.
func (p *ValueProcessor) ProcessValue(value any) any {
	if value == nil {
		return nil
	}

	// Direct uint64 conversion
	if uintValue, ok := value.(uint64); ok {
		// Return a map with the string value and type metadata
		return map[string]any{
			"value":                  strconv.FormatUint(uintValue, 10),
			TypeMetadataPrefix + "0": NumericTypeMeta,
		}
	}

	// Handle time.Time values
	if timeValue, ok := value.(time.Time); ok {
		return timeValue.Format(time.RFC3339Nano)
	}

	// Handle slices
	refValue := reflect.ValueOf(value)
	switch refValue.Kind() {
	case reflect.Slice:
		if refValue.Type().Elem().Kind() == reflect.Uint64 {
			// Special case for []uint64
			result := make([]any, refValue.Len())
			for i := range refValue.Len() {
				// Store each uint64 with type metadata
				result[i] = map[string]any{
					"value":                  strconv.FormatUint(refValue.Index(i).Uint(), 10),
					TypeMetadataPrefix + "0": NumericTypeMeta,
				}
			}
			return result
		}
		// Process each element in the slice
		result := make([]any, refValue.Len())
		for i := range refValue.Len() {
			result[i] = p.ProcessValue(refValue.Index(i).Interface())
		}
		return result

	case reflect.Map:
		// Process map keys and values
		result := make(map[string]any)
		for _, key := range refValue.MapKeys() {
			// Convert map keys to strings
			var keyStr string

			// Handle numeric keys
			switch key.Kind() {
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				keyStr = strconv.FormatUint(key.Uint(), 10)
				result[TypeMetadataPrefix+keyStr] = NumericTypeMeta
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				keyStr = strconv.FormatInt(key.Int(), 10)
				result[TypeMetadataPrefix+keyStr] = NumericTypeMeta
			default:
				keyStr = fmt.Sprintf("%v", key.Interface())
			} //exhaustive:ignore

			// Process map values
			result[keyStr] = p.ProcessValue(refValue.MapIndex(key).Interface())
		}
		return result

	case reflect.Struct:
		// Process struct fields
		result := make(map[string]any)
		refType := refValue.Type()

		// Process the fields of this struct and any embedded structs
		for i := range refType.NumField() {
			field := refType.Field(i)

			// Skip unexported fields
			if !field.IsExported() {
				continue
			}

			fieldValue := refValue.Field(i)

			// Handle embedded or regular fields
			if field.Anonymous {
				p.handleEmbeddedField(field, fieldValue, result)
			} else {
				p.handleNonEmbeddedField(field, fieldValue, result)
			}
		}
		return result

	case reflect.Ptr:
		if !refValue.IsNil() {
			return p.ProcessValue(refValue.Elem().Interface())
		}
	} //exhaustive:ignore

	return value
}

// handleEmbeddedField processes an anonymous (embedded) struct field.
func (p *ValueProcessor) handleEmbeddedField(field reflect.StructField, fieldValue reflect.Value, result map[string]any) {
	// Skip handling zero-valued embedded fields
	if fieldValue.IsZero() {
		return
	}

	// Extract the JSON tag if present
	jsonTag := field.Tag.Get("json")
	embeddedValue := fieldValue.Interface()
	processed := p.ProcessValue(embeddedValue)

	// Handle fields with explicit JSON tags
	if jsonTag != "" && jsonTag != "-" {
		// Extract the name part from the json tag (before any comma)
		tagName := strings.SplitN(jsonTag, ",", 2)[0]
		result[tagName] = processed
		return
	}

	// For embedded structs without JSON tags, flatten their fields into parent
	if embeddedMap, ok := processed.(map[string]any); ok {
		for k, v := range embeddedMap {
			result[k] = v
		}
	}
}

// handleNonEmbeddedField processes a regular (non-embedded) struct field.
func (p *ValueProcessor) handleNonEmbeddedField(field reflect.StructField, fieldValue reflect.Value, result map[string]any) {
	jsonTag := field.Tag.Get("json")

	// Skip fields marked with json:"-"
	if jsonTag == "-" {
		return
	}

	// Determine field name from JSON tag or struct field name
	fieldName := field.Name
	if jsonTag != "" {
		// Extract the name part from the json tag (before any comma)
		parts := strings.SplitN(jsonTag, ",", 2)
		if parts[0] != "" {
			fieldName = parts[0]
		}
	}

	// Process field value
	result[fieldName] = p.ProcessValue(fieldValue.Interface())
}
