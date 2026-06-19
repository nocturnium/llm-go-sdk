// Package all registers all chat-capable providers via blank imports.
package all

import (
	// Each blank import registers its provider with llms.RegisterProvider via the
	// provider package's init(), so importing this package wires up the by-name
	// registry (llms.New / llms.NewFromEnv) for all chat providers.
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/anthropic"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/azure"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/cerebras"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/deepseek"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/featherless"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/fireworks"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/gemini"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/groq"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/llamacpp"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/mistral"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/ollama"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/openai"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/perplexity"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/runpod"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/synthetic"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/togetherai"
	_ "github.com/nocturnium/llm-go-sdk/v3/pkg/providers/zai"
)
