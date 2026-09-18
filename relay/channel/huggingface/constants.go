package huggingface

const ChannelName = "huggingface"

const DefaultBaseURL = "https://router.huggingface.co/v1"

var ModelList = []string{
	// DeepSeek
	"deepseek-ai/DeepSeek-R1",
	"deepseek-ai/DeepSeek-R1-0528",
	"deepseek-ai/DeepSeek-V3-0324",
	// Qwen
	"Qwen/Qwen3-235B-A22B",
	"Qwen/Qwen3-32B",
	"Qwen/Qwen3-14B",
	"Qwen/Qwen3-8B",
	"Qwen/Qwen3-4B",
	"Qwen/Qwen3-1.7B",
	"Qwen/Qwen3-0.6B",
	"Qwen/Qwen2.5-72B-Instruct",
	"Qwen/Qwen2.5-32B-Instruct",
	"Qwen/Qwen2.5-14B-Instruct",
	"Qwen/Qwen2.5-7B-Instruct",
	"Qwen/Qwen2.5-Coder-32B-Instruct",
	"Qwen/Qwen2.5-Coder-7B-Instruct",
	// Meta Llama
	"meta-llama/Llama-4-Maverick-17B-128E-Instruct",
	"meta-llama/Llama-4-Scout-17B-16E-Instruct",
	"meta-llama/Llama-3.3-70B-Instruct",
	"meta-llama/Llama-3.1-405B-Instruct",
	"meta-llama/Llama-3.1-70B-Instruct",
	"meta-llama/Llama-3.1-8B-Instruct",
	"meta-llama/Llama-3-70B-Instruct",
	"meta-llama/Llama-3-8B-Instruct",
	// Mistral
	"MistralAI/Mistral-Small-3.1-24B-Instruct-2503",
	"MistralAI/Mistral-Large-Instruct-2411",
	"MistralAI/Mistral-Small-3.2-24B-Instruct-2506",
	// Google
	"google/gemma-3-27b-it",
	"google/gemma-3-12b-it",
	"google/gemma-3-4b-it",
	// Microsoft
	"microsoft/Phi-4-reasoning-plus",
	"microsoft/Phi-4-reasoning",
	"microsoft/Phi-4",
	"microsoft/Phi-3.5-mini-instruct",
	// Cohere
	"CohereForAI/c4ai-command-a-03-2025",
	// Nous
	"NousResearch/Hermes-3-Llama-3.1-405B-FP8",
	"NousResearch/Hermes-3-Llama-3.1-70B-FP8",
}
