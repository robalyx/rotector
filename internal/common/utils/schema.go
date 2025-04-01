package utils

import "github.com/invopop/jsonschema"

// GenerateSchema creates a JSON schema for validating data structures.
// It uses reflection to analyze the type T and generate a corresponding JSON schema.
// The generated schema is strict (no additional properties allowed) and fully inlined
// (no references to definitions).
func GenerateSchema[T any]() any {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}
