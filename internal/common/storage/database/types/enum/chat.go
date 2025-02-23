package enum

// ChatModel represents different chat models.
//
//go:generate go tool enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGeminiFlash1_5_8B ChatModel = iota // gemini-1.5-flash-8b
)
