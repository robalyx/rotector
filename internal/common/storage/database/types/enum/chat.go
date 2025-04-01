package enum

// ChatModel represents different chat models.
//
//go:generate go tool enumer -type=ChatModel -trimprefix=ChatModel -linecomment
type ChatModel int

const (
	ChatModelGemini2_0Flash  ChatModel = iota // google/gemini-2.0-flash-001
	ChatModelGPT4oMini                        // openai/gpt-4o-mini-2024-07-18
	ChatModelQwQ32B                           // deepinfra/Qwen/QwQ-32B
	ChatModelDeepseekQwen32B                  // novita/deepseek/deepseek-r1-distill-qwen-32b
	ChatModelDeepseekR1                       // deepinfra/deepseek-ai/DeepSeek-R1-Distill-Llama-70B
	ChatModelLlama3_3_70B                     // nebius/meta-llama/Llama-3.3-70B-Instruct
)
