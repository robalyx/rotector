package translator

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/jaxron/axonet/pkg/client"
)

// Translator handles text translation between languages by making requests
// to the Google Translate API through a HTTP client.
type Translator struct {
	client *client.Client
}

// New creates a Translator with the provided HTTP client for making
// translation requests.
func New(client *client.Client) *Translator {
	return &Translator{client: client}
}

// Translate sends text to Google Translate API for translation.
// The response is parsed to extract just the translated text.
func (t *Translator) Translate(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
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

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Unmarshal the response body
	var result []interface{}
	err = sonic.Unmarshal(body, &result)
	if err != nil {
		return "", err
	}

	// Build the translated text from response segments
	var translatedText strings.Builder
	for _, slice := range result[0].([]interface{}) {
		translatedText.WriteString(slice.([]interface{})[0].(string))
	}

	return translatedText.String(), nil
}
