package enum

// ChatModel represents different chat models.
//
//go:generate go tool enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGemini2_5Flash  ChatModel = iota // gemini-2.5-flash
	ChatModelQwen3_235bA22b                   // qwen-3-235b-a22b
	ChatModelDeepseekV3_0324                  // deepseek-v3-0324
	ChatModelGPT4_1Mini                       // gpt-4.1-mini
	ChatModelo4Mini                           // o4-mini
	ChatModelo4MiniHigh                       // o4-mini-high
)
