package client

import "strings"

// geminiSafetySettings defines safety thresholds for Gemini models.
var geminiSafetySettings = []map[string]any{
	{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "OFF"},
}

// ExtraFieldsSettings contains configuration for OpenAI API extra fields.
type ExtraFieldsSettings struct {
	MaxTokens        int
	ReasoningEnabled bool
	ReasoningTokens  int
	SafetySettings   []map[string]any
	ProviderOptions  map[string]any
}

// NewExtraFieldsSettings creates default settings.
func NewExtraFieldsSettings() *ExtraFieldsSettings {
	return &ExtraFieldsSettings{
		MaxTokens:        8192,
		ReasoningEnabled: false,
		ReasoningTokens:  1,
	}
}

// ForModel applies model-specific settings (e.g., Gemini safety settings).
func (s *ExtraFieldsSettings) ForModel(modelName string) *ExtraFieldsSettings {
	if strings.Contains(strings.ToLower(modelName), "gemini") {
		s.SafetySettings = geminiSafetySettings
		s.ProviderOptions = map[string]any{
			"gateway": map[string]any{
				"only": []string{"vertex"},
			},
		}
	}

	return s
}

// WithReasoning enables reasoning with the specified max tokens.
func (s *ExtraFieldsSettings) WithReasoning(maxTokens int) *ExtraFieldsSettings {
	s.ReasoningEnabled = true
	s.ReasoningTokens = maxTokens

	return s
}

// Build converts the settings to a map for the OpenAI API.
func (s *ExtraFieldsSettings) Build() map[string]any {
	fields := map[string]any{
		"max_tokens": s.MaxTokens,
		"reasoning": map[string]any{
			"enabled":    s.ReasoningEnabled,
			"max_tokens": s.ReasoningTokens,
		},
	}

	if s.SafetySettings != nil {
		fields["safety_settings"] = s.SafetySettings
	}

	if s.ProviderOptions != nil {
		fields["providerOptions"] = s.ProviderOptions
	}

	return fields
}
