package enum

// ChatModel represents different chat models.
//
//go:generate go tool enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGemini2_0Flash  ChatModel = iota // gemini-2.0-flash
	ChatModelGPT4oMini                        // gpt-4o-mini
	ChatModelQwQ32B                           // qwq-32B
	ChatModelDeepseekQwen32B                  // deepseek-r1-distill-qwen-32b
	ChatModelDeepseekR1                       // deepseek-r1-distill-llama-70B
	ChatModelLlama3_3_70B                     // llama-3.3-70B-instruct
)
