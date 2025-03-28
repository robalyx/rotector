package enum

// ChatModel represents different chat models.
//
//go:generate go tool enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGeminiFlash1_5_8B  ChatModel = iota // gemini-1.5-flash-8b
	ChatModelGemini1_5Flash                      // gemini-1.5-flash
	ChatModelGemini2_0FlashLite                  // gemini-2.0-flash-lite
	ChatModelGemini2_0Flash                      // gemini-2.0-flash
)
