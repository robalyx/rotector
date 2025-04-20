package enum

// ChatModel represents different chat models.
//
//go:generate go tool enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGemini2_5Flash  ChatModel = iota // gemini-2.5-flash-thinking
	ChatModelGemini2_5Pro                     // gemini-2.5-pro
	ChatModelQwQ32B                           // qwq-32B
	ChatModelDeepseekV3_0324                  // deepseek-v3-0324
	ChatModelGPT4oMini                        // gpt-4o-mini
	ChatModelo4Mini                           // o4-mini
)
