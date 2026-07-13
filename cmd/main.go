// Package main provides a CLI tool for testing and demonstrating the llm-go-sdk.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/azure"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/cerebras"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/deepseek"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/featherless"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/fireworks"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/groq"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/llamacpp"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/mistral"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/ollama"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/perplexity"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/runpod"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/synthetic"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/togetherai"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/zai"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type commandContext struct {
	args        []string
	provider    string
	model       string
	system      string
	temperature float64
	maxTokens   int
}

func (c commandContext) nArg() int {
	return len(c.args)
}

func run(args []string, stdout, stderr io.Writer) int {
	if err := runCommand(args, stdout, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func runCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	case "--version", "-v":
		_, _ = fmt.Fprintf(stdout, "llms-cli version %s\n", llms.Version)
		return nil
	case "chat":
		return runChat(args[1:], stdout)
	case "complete":
		return runComplete(args[1:], stdout)
	case "providers":
		return runProviders(args[1:], stdout)
	case "tool-demo":
		return runToolDemo(args[1:], stdout)
	case "version":
		return runVersion(args[1:], stdout)
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `llms-cli - CLI tool for testing LLM providers

Usage:
  llms-cli [--version] <command> [options] [arguments]

Commands:
  chat        Send a chat message to an LLM provider
  complete    Send a completion prompt to an LLM provider
  providers   List available providers and their default models
  tool-demo   Demonstrate tool calling with a weather example
  version     Print detailed version information

Use "llms-cli <command> -h" for command-specific help.
`)
}

func newFlagSet(name, usage string, stdout io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stdout, "Usage: llms-cli %s [options] [arguments]\n\n%s\n\nOptions:\n", name, usage)
		fs.SetOutput(stdout)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
	return fs
}

func parseCommand(fs *flag.FlagSet, args []string) (bool, error) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func runChat(args []string, stdout io.Writer) error {
	ctx := commandContext{
		provider:    envString("LLM_PROVIDER", ""),
		model:       envString("LLM_MODEL", ""),
		system:      envString("LLM_SYSTEM", "You are a helpful assistant."),
		temperature: envFloat64("LLM_TEMPERATURE", 0.7),
		maxTokens:   envInt("LLM_MAX_TOKENS", 1024),
	}
	fs := newFlagSet("chat", "Send a chat message to an LLM provider", stdout)
	fs.StringVar(&ctx.provider, "provider", ctx.provider, "LLM provider (openai, anthropic, gemini, togetherai, featherless) [$LLM_PROVIDER]")
	fs.StringVar(&ctx.provider, "p", ctx.provider, "LLM provider (shorthand)")
	fs.StringVar(&ctx.model, "model", ctx.model, "Model to use (uses provider default if not specified) [$LLM_MODEL]")
	fs.StringVar(&ctx.model, "m", ctx.model, "Model to use (shorthand)")
	fs.StringVar(&ctx.system, "system", ctx.system, "System prompt [$LLM_SYSTEM]")
	fs.StringVar(&ctx.system, "s", ctx.system, "System prompt (shorthand)")
	fs.Float64Var(&ctx.temperature, "temperature", ctx.temperature, "Temperature for generation [$LLM_TEMPERATURE]")
	fs.Float64Var(&ctx.temperature, "t", ctx.temperature, "Temperature for generation (shorthand)")
	fs.IntVar(&ctx.maxTokens, "max-tokens", ctx.maxTokens, "Maximum tokens to generate [$LLM_MAX_TOKENS]")
	fs.IntVar(&ctx.maxTokens, "n", ctx.maxTokens, "Maximum tokens to generate (shorthand)")
	help, err := parseCommand(fs, args)
	if err != nil || help {
		return err
	}
	if ctx.provider == "" {
		return fmt.Errorf("required flag \"provider\" not set")
	}
	ctx.args = fs.Args()
	return chatAction(ctx)
}

func runComplete(args []string, stdout io.Writer) error {
	ctx := commandContext{
		temperature: 0.7,
		maxTokens:   1024,
	}
	fs := newFlagSet("complete", "Send a completion prompt to an LLM provider", stdout)
	fs.StringVar(&ctx.provider, "provider", "", "LLM provider (openai, anthropic, gemini, togetherai, featherless)")
	fs.StringVar(&ctx.provider, "p", "", "LLM provider (shorthand)")
	fs.StringVar(&ctx.model, "model", "", "Model to use (uses provider default if not specified)")
	fs.StringVar(&ctx.model, "m", "", "Model to use (shorthand)")
	fs.Float64Var(&ctx.temperature, "temperature", ctx.temperature, "Temperature for generation")
	fs.Float64Var(&ctx.temperature, "t", ctx.temperature, "Temperature for generation (shorthand)")
	fs.IntVar(&ctx.maxTokens, "max-tokens", ctx.maxTokens, "Maximum tokens to generate")
	fs.IntVar(&ctx.maxTokens, "n", ctx.maxTokens, "Maximum tokens to generate (shorthand)")
	help, err := parseCommand(fs, args)
	if err != nil || help {
		return err
	}
	if ctx.provider == "" {
		return fmt.Errorf("required flag \"provider\" not set")
	}
	ctx.args = fs.Args()
	return completeAction(ctx)
}

func runProviders(args []string, stdout io.Writer) error {
	fs := newFlagSet("providers", "List available providers and their default models", stdout)
	help, err := parseCommand(fs, args)
	if err != nil || help {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("providers does not accept arguments")
	}
	return providersAction()
}

func runToolDemo(args []string, stdout io.Writer) error {
	ctx := commandContext{
		provider: envString("LLM_PROVIDER", ""),
		model:    envString("LLM_MODEL", ""),
	}
	fs := newFlagSet("tool-demo", "Demonstrate tool calling with a weather example", stdout)
	fs.StringVar(&ctx.provider, "provider", ctx.provider, "LLM provider (openai, anthropic, gemini) [$LLM_PROVIDER]")
	fs.StringVar(&ctx.provider, "p", ctx.provider, "LLM provider (shorthand)")
	fs.StringVar(&ctx.model, "model", ctx.model, "Model to use (uses provider default if not specified) [$LLM_MODEL]")
	fs.StringVar(&ctx.model, "m", ctx.model, "Model to use (shorthand)")
	help, err := parseCommand(fs, args)
	if err != nil || help {
		return err
	}
	if ctx.provider == "" {
		return fmt.Errorf("required flag \"provider\" not set")
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("tool-demo does not accept arguments")
	}
	return toolDemoAction(ctx)
}

func runVersion(args []string, stdout io.Writer) error {
	fs := newFlagSet("version", "Print detailed version information", stdout)
	help, err := parseCommand(fs, args)
	if err != nil || help {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("version does not accept arguments")
	}
	_, _ = fmt.Fprintln(stdout, llms.VersionInfo())
	return nil
}

func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envFloat64(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

func providersAction() error {
	type row struct{ name, model, env string }
	chat := []row{
		{"openai", "gpt-4o", "OPENAI_API_KEY"},
		{"anthropic", "claude-sonnet-4-20250514", "ANTHROPIC_API_KEY"},
		{"azure", "(deployment)", "AZURE_OPENAI_API_KEY"},
		{"cerebras", "llama3.1-70b", "CEREBRAS_API_KEY"},
		{"deepseek", "deepseek-chat", "DEEPSEEK_API_KEY"},
		{"featherless", "Qwen/Qwen3-32B", "FEATHERLESS_API_KEY"},
		{"fireworks", "llama-v3p1-70b-instruct", "FIREWORKS_API_KEY"},
		{"gemini", "gemini-2.5-flash", "GEMINI_API_KEY / GOOGLE_API_KEY"},
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

func chatAction(c commandContext) error {
	if c.nArg() == 0 {
		return fmt.Errorf("please provide a message as an argument")
	}

	message := strings.Join(c.args, " ")
	provider := c.provider
	model := c.model
	systemPrompt := c.system
	temperature := c.temperature
	maxTokens := c.maxTokens

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

func completeAction(c commandContext) error {
	if c.nArg() == 0 {
		return fmt.Errorf("please provide a prompt as an argument")
	}

	prompt := strings.Join(c.args, " ")
	provider := c.provider
	model := c.model
	temperature := c.temperature
	maxTokens := c.maxTokens

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

func toolDemoAction(c commandContext) error {
	provider := c.provider
	model := c.model

	client, err := createClient(provider, model)
	if err != nil {
		return err
	}

	fmt.Printf("Provider: %s\n", client.Provider())
	fmt.Printf("Model: %s\n", client.Model())
	fmt.Println("---")
	fmt.Println("Demonstrating tool calling with a weather tool...")
	fmt.Println()

	// Define the weather tool.
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

	// Initial message asking about weather.
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a helpful assistant that can check the weather. Use the get_current_weather tool when asked about weather."},
		{Role: llms.RoleUser, Content: "What's the weather like in San Francisco?"},
	}

	fmt.Println("User: What's the weather like in San Francisco?")
	fmt.Println()

	// First call - model should request tool call.
	resp, err := client.GenerateContent(
		context.Background(),
		messages,
		llms.WithTools([]llms.Tool{weatherTool}),
		llms.WithTemperature(0),
	)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	// Check if the model requested a tool call.
	if len(resp.ToolCalls) == 0 {
		fmt.Println("Model response (no tool call):", resp.Content)
		return nil
	}

	fmt.Printf("Model requested tool call: %s\n", resp.ToolCalls[0].Function.Name)
	fmt.Printf("Arguments: %s\n", resp.ToolCalls[0].Function.Arguments)
	fmt.Println()

	// Parse arguments.
	var args struct {
		Location string `json:"location"`
		Unit     string `json:"unit"`
	}
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &args); err != nil {
		return fmt.Errorf("failed to parse arguments: %w", err)
	}

	// Simulate tool response.
	unit := args.Unit
	if unit == "" {
		unit = "fahrenheit"
	}
	weatherResult := fmt.Sprintf(`{"location": "%s", "temperature": 72, "unit": "%s", "condition": "sunny", "humidity": 45}`, args.Location, unit)

	fmt.Printf("Tool response: %s\n", weatherResult)
	fmt.Println()

	// Add assistant message with tool calls and tool response.
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

	// Second call - model should respond with the weather info.
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
