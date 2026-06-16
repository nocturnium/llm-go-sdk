// Package main provides a CLI tool for testing and demonstrating the llm-go-sdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/azure"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/cerebras"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/deepseek"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/featherless"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/fireworks"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/groq"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/llamacpp"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/mistral"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/ollama"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/perplexity"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/runpod"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/synthetic"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/togetherai"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/zai"
)

func main() {
	app := newApp()

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newApp() *cli.App {
	return &cli.App{
		Name:    "llms-cli",
		Version: llms.Version,
		Usage:   "CLI tool for testing LLM providers",
		Commands: []*cli.Command{
			{
				Name:  "chat",
				Usage: "Send a chat message to an LLM provider",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "provider",
						Aliases:  []string{"p"},
						EnvVars:  []string{"LLM_PROVIDER"},
						Usage:    "LLM provider (openai, anthropic, gemini, togetherai, featherless)",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "model",
						Aliases: []string{"m"},
						EnvVars: []string{"LLM_MODEL"},
						Usage:   "Model to use (uses provider default if not specified)",
					},
					&cli.StringFlag{
						Name:    "system",
						Aliases: []string{"s"},
						Usage:   "System prompt",
						EnvVars: []string{"LLM_SYSTEM"},
						Value:   "You are a helpful assistant.",
					},
					&cli.Float64Flag{
						Name:    "temperature",
						Aliases: []string{"t"},
						EnvVars: []string{"LLM_TEMPERATURE"},
						Usage:   "Temperature for generation",
						Value:   0.7,
					},
					&cli.IntFlag{
						Name:    "max-tokens",
						Aliases: []string{"n"},
						EnvVars: []string{"LLM_MAX_TOKENS"},
						Usage:   "Maximum tokens to generate",
						Value:   1024,
					},
				},
				Action: chatAction,
			},
			{
				Name:  "complete",
				Usage: "Send a completion prompt to an LLM provider",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "provider",
						Aliases:  []string{"p"},
						Usage:    "LLM provider (openai, anthropic, gemini, togetherai, featherless)",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "model",
						Aliases: []string{"m"},
						Usage:   "Model to use (uses provider default if not specified)",
					},
					&cli.Float64Flag{
						Name:    "temperature",
						Aliases: []string{"t"},
						Usage:   "Temperature for generation",
						Value:   0.7,
					},
					&cli.IntFlag{
						Name:    "max-tokens",
						Aliases: []string{"n"},
						Usage:   "Maximum tokens to generate",
						Value:   1024,
					},
				},
				Action: completeAction,
			},
			{
				Name:  "providers",
				Usage: "List available providers and their default models",
				Action: func(_ *cli.Context) error {
					type row struct{ name, model, env string }
					chat := []row{
						{"openai", "gpt-4o", "OPENAI_API_KEY"},
						{"anthropic", "claude-sonnet-4-20250514", "ANTHROPIC_API_KEY"},
						{"azure", "(deployment)", "AZURE_OPENAI_API_KEY"},
						{"cerebras", "llama3.1-70b", "CEREBRAS_API_KEY"},
						{"deepseek", "deepseek-chat", "DEEPSEEK_API_KEY"},
						{"featherless", "Qwen/Qwen3-32B", "FEATHERLESS_API_KEY"},
						{"fireworks", "llama-v3p1-70b-instruct", "FIREWORKS_API_KEY"},
						{"gemini", "gemini-2.0-flash", "GEMINI_API_KEY / GOOGLE_API_KEY"},
						{"groq", "llama-3.3-70b-versatile", "GROQ_API_KEY"},
						{"llamacpp", "(from server /props)", "LLAMA_CPP_HOST"},
						{"mistral", "mistral-large-latest", "MISTRAL_API_KEY"},
						{"ollama", "llama3.2", "OLLAMA_HOST"},
						{"perplexity", "sonar", "PERPLEXITY_API_KEY / PPLX_API_KEY"},
						{"runpod", "(endpoint deployment)", "RUNPOD_API_KEY"},
						{"synthetic", "Qwen3-Coder-480B", "SYNTHETIC_API_KEY"},
						{"togetherai", "Llama-3.3-70B-Instruct-Turbo", "TOGETHER_API_KEY"},
						{"zai", "glm-4.7", "ZAI_API_KEY"},
					}
					fmt.Println("Available chat providers:")
					fmt.Println()
					fmt.Printf("  %-12s %-32s %s\n", "PROVIDER", "DEFAULT MODEL", "ENV VAR(S)")
					fmt.Printf("  %-12s %-32s %s\n", "--------", "-------------", "----------")
					for _, r := range chat {
						fmt.Printf("  %-12s %-32s %s\n", r.name, r.model, r.env)
					}
					fmt.Println()
					fmt.Println("Embeddings/reranking only (not a chat provider): infinity (INFINITY_API_KEY)")
					fmt.Println("All chat providers also fall back to LLM_API_KEY.")
					return nil
				},
			},
			{
				Name:  "tool-demo",
				Usage: "Demonstrate tool calling with a weather example",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "provider",
						Aliases:  []string{"p"},
						EnvVars:  []string{"LLM_PROVIDER"},
						Usage:    "LLM provider (openai, anthropic, gemini)",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "model",
						Aliases: []string{"m"},
						EnvVars: []string{"LLM_MODEL"},
						Usage:   "Model to use (uses provider default if not specified)",
					},
				},
				Action: toolDemoAction,
			},
			{
				Name:  "version",
				Usage: "Print detailed version information",
				Action: func(c *cli.Context) error {
					_, err := fmt.Fprintln(c.App.Writer, llms.VersionInfo())
					return err
				},
			},
		},
	}
}

func createClient(provider, model string) (llms.LLM, error) {
	switch strings.ToLower(provider) {
	case "openai":
		return openai.New(modelOpts(model, openai.WithModel)...)
	case "anthropic":
		return anthropic.New(modelOpts(model, anthropic.WithModel)...)
	case "azure":
		// Azure addresses models by deployment name rather than model name.
		return azure.New(modelOpts(model, azure.WithDeployment)...)
	case "cerebras":
		return cerebras.New(modelOpts(model, cerebras.WithModel)...)
	case "deepseek":
		return deepseek.New(modelOpts(model, deepseek.WithModel)...)
	case "featherless":
		return featherless.New(modelOpts(model, featherless.WithModel)...)
	case "fireworks":
		return fireworks.New(modelOpts(model, fireworks.WithModel)...)
	case "gemini":
		return gemini.New(modelOpts(model, gemini.WithModel)...)
	case "groq":
		return groq.New(modelOpts(model, groq.WithModel)...)
	case "llamacpp", "llama.cpp":
		return llamacpp.New(modelOpts(model, llamacpp.WithModel)...)
	case "mistral":
		return mistral.New(modelOpts(model, mistral.WithModel)...)
	case "ollama":
		return ollama.New(modelOpts(model, ollama.WithModel)...)
	case "perplexity":
		return perplexity.New(modelOpts(model, perplexity.WithModel)...)
	case "runpod":
		return runpod.New(modelOpts(model, runpod.WithModel)...)
	case "synthetic":
		return synthetic.New(modelOpts(model, synthetic.WithModel)...)
	case "togetherai":
		return togetherai.New(modelOpts(model, togetherai.WithModel)...)
	case "zai":
		return zai.New(modelOpts(model, zai.WithModel)...)
	default:
		return nil, fmt.Errorf("unknown or non-chat provider: %s (note: infinity is embeddings/reranking only)", provider)
	}
}

// modelOpts returns the provider option slice, applying WithModel only when a
// model override is supplied. The generic signature keeps the per-provider
// Option types intact.
func modelOpts[O any](model string, withModel func(string) O) []O {
	if model == "" {
		return nil
	}
	return []O{withModel(model)}
}

func chatAction(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("please provide a message as an argument")
	}

	message := strings.Join(c.Args().Slice(), " ")
	provider := c.String("provider")
	model := c.String("model")
	systemPrompt := c.String("system")
	temperature := c.Float64("temperature")
	maxTokens := c.Int("max-tokens")

	client, err := createClient(provider, model)
	if err != nil {
		return err
	}

	fmt.Printf("Provider: %s\n", client.Provider())
	fmt.Printf("Model: %s\n", client.Model())
	fmt.Println("---")

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: systemPrompt},
		{Role: llms.RoleUser, Content: message},
	}

	resp, err := client.GenerateContent(
		context.Background(),
		messages,
		llms.WithTemperature(temperature),
		llms.WithMaxTokens(maxTokens),
	)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	fmt.Println(resp.Content)
	fmt.Println("---")
	fmt.Printf("Finish Reason: %s\n", resp.FinishReason)
	fmt.Printf("Usage: prompt=%d, completion=%d, total=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)

	return nil
}

func completeAction(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("please provide a prompt as an argument")
	}

	prompt := strings.Join(c.Args().Slice(), " ")
	provider := c.String("provider")
	model := c.String("model")
	temperature := c.Float64("temperature")
	maxTokens := c.Int("max-tokens")

	client, err := createClient(provider, model)
	if err != nil {
		return err
	}

	fmt.Printf("Provider: %s\n", client.Provider())
	fmt.Printf("Model: %s\n", client.Model())
	fmt.Println("---")

	resp, err := llms.Call(
		context.Background(),
		client,
		prompt,
		llms.WithTemperature(temperature),
		llms.WithMaxTokens(maxTokens),
	)
	if err != nil {
		return fmt.Errorf("completion failed: %w", err)
	}

	fmt.Println(resp)

	return nil
}

func toolDemoAction(c *cli.Context) error {
	provider := c.String("provider")
	model := c.String("model")

	client, err := createClient(provider, model)
	if err != nil {
		return err
	}

	fmt.Printf("Provider: %s\n", client.Provider())
	fmt.Printf("Model: %s\n", client.Model())
	fmt.Println("---")
	fmt.Println("Demonstrating tool calling with a weather tool...")
	fmt.Println()

	// Define the weather tool
	weatherTool := llms.NewFunctionTool(
		"get_current_weather",
		"Get the current weather in a given location",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": "The city and state, e.g. San Francisco, CA",
				},
				"unit": map[string]any{
					"type": "string",
					"enum": []string{"celsius", "fahrenheit"},
				},
			},
			"required": []string{"location"},
		},
	)

	// Initial message asking about weather
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a helpful assistant that can check the weather. Use the get_current_weather tool when asked about weather."},
		{Role: llms.RoleUser, Content: "What's the weather like in San Francisco?"},
	}

	fmt.Println("User: What's the weather like in San Francisco?")
	fmt.Println()

	// First call - model should request tool call
	resp, err := client.GenerateContent(
		context.Background(),
		messages,
		llms.WithTools([]llms.Tool{weatherTool}),
		llms.WithTemperature(0),
	)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	// Check if the model requested a tool call
	if len(resp.ToolCalls) == 0 {
		fmt.Println("Model response (no tool call):", resp.Content)
		return nil
	}

	fmt.Printf("Model requested tool call: %s\n", resp.ToolCalls[0].Function.Name)
	fmt.Printf("Arguments: %s\n", resp.ToolCalls[0].Function.Arguments)
	fmt.Println()

	// Parse arguments
	var args struct {
		Location string `json:"location"`
		Unit     string `json:"unit"`
	}
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &args); err != nil {
		return fmt.Errorf("failed to parse arguments: %w", err)
	}

	// Simulate tool response
	unit := args.Unit
	if unit == "" {
		unit = "fahrenheit"
	}
	weatherResult := fmt.Sprintf(`{"location": "%s", "temperature": 72, "unit": "%s", "condition": "sunny", "humidity": 45}`, args.Location, unit)

	fmt.Printf("Tool response: %s\n", weatherResult)
	fmt.Println()

	// Add assistant message with tool calls and tool response
	messages = append(messages,
		llms.Message{
			Role:      llms.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		},
		llms.Message{
			Role:       llms.RoleTool,
			Content:    weatherResult,
			ToolCallID: resp.ToolCalls[0].ID,
			Name:       resp.ToolCalls[0].Function.Name,
		},
	)

	// Second call - model should respond with the weather info
	resp, err = client.GenerateContent(
		context.Background(),
		messages,
		llms.WithTools([]llms.Tool{weatherTool}),
		llms.WithTemperature(0),
	)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	fmt.Println("Assistant:", resp.Content)
	fmt.Println("---")
	fmt.Printf("Finish Reason: %s\n", resp.FinishReason)
	fmt.Printf("Usage: prompt=%d, completion=%d, total=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)

	return nil
}
