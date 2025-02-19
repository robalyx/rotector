package translator

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/bytedance/sonic"
)

// TranslateLanguage translates text between natural languages using Google Translate API.
// sourceLang and targetLang should be ISO 639-1 language codes (e.g., "en" for English).
// Returns the original text for simple content, otherwise returns the translated text.
func (t *Translator) TranslateLanguage(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
	// Send request to Google Translate API
	resp, err := t.client.NewRequest().
		Method(http.MethodGet).
		URL("https://translate.google.com/translate_a/single").
		Query("client", "gtx").
		Query("sl", sourceLang).
		Query("tl", targetLang).
		Query("dt", "t").
		Query("q", text).
		Do(ctx)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Read and parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result []interface{}
	if err := sonic.Unmarshal(body, &result); err != nil {
		return "", err
	}

	// Extract translated text from the response
	var translatedText strings.Builder
	for _, slice := range result[0].([]interface{}) {
		translatedText.WriteString(slice.([]interface{})[0].(string))
	}

	return translatedText.String(), nil
}
