// Package all registers all chat-capable providers via blank imports.
package all

import (
	// Each blank import registers its provider with llms.RegisterProvider via the
	// provider package's init(), so importing this package wires up the by-name
	// registry (llms.New / llms.NewFromEnv) for all chat providers.
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/anthropic"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/azure"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/cerebras"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/deepseek"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/featherless"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/fireworks"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/gemini"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/groq"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/llamacpp"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/mistral"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/ollama"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/openai"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/perplexity"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/runpod"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/synthetic"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/togetherai"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/zai"
)
