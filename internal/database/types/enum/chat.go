package enum

// ChatModel represents different chat models.
//
//go:generate go tool enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGemini2_0Flash  ChatModel = iota // gemini-2.0-flash
	ChatModelGemini2_5Pro                     // gemini-2.5-pro
	ChatModelDeepseekR1                       // deepseek-r1
	ChatModelDeepseekV3_0324                  // deepseek-v3-0324
	ChatModelGPT4oMini                        // gpt-4o-mini
)
