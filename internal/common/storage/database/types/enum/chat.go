package enum

// ChatModel represents different chat models.
//
//go:generate enumer -type=ChatModel -trimprefix=ChatModel
type ChatModel int

const (
	ChatModelGeminiPro ChatModel = iota
	ChatModelGeminiFlash
	ChatModelGeminiFlash8B
)
