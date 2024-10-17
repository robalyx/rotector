package translator

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/jaxron/axonet/pkg/client"
)

// Translator is a translator that translates text from one language to another.
type Translator struct {
	client *client.Client
}

// New creates a new Translator.
func New(client *client.Client) *Translator {
	return &Translator{client: client}
}

// Translate translates the given text from the source language to the target language.
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

	// Build the translated text
	var translatedText strings.Builder
	for _, slice := range result[0].([]interface{}) {
		translatedText.WriteString(slice.([]interface{})[0].(string))
	}

	return translatedText.String(), nil
}
