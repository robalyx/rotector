package enum

// ChatModel represents different chat models.
//
//go:generate enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGeminiFlash2_0    ChatModel = iota // gemini-2.0-flash-001
	ChatModelGeminiFlash1_5                     // gemini-1.5-flash
	ChatModelGeminiFlash1_5_8B                  // gemini-1.5-flash-8b
)
