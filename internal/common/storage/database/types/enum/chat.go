package enum

// ChatModel represents different chat models.
//
//go:generate enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGeminiPro     ChatModel = iota // gemini-1.5-pro-latest
	ChatModelGeminiFlash                    // gemini-1.5-flash-latest
	ChatModelGeminiFlash8B                  // gemini-1.5-flash-8b-latest
)
