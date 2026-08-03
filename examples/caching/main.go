// Example: Prompt caching
//
// Demonstrates cross-provider prompt caching: enabling automatic caching with
// WithCache, marking explicit per-message breakpoints with Message.CacheControl,
// and reading cache token usage. Caching a large, stable prefix (a long system
// prompt, tool definitions, or a document) and reusing it across calls cuts both
// latency and cost — cache reads are billed at a steep discount.
//
// Run with: ANTHROPIC_API_KEY=... go run ./examples/caching
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/anthropic"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := anthropic.New(anthropic.WithModel("claude-3-5-sonnet-20241022"))
	if err != nil {
		log.Fatalf("anthropic: %v", err)
	}

	// A large, stable system prompt is the ideal thing to cache.
	systemPrompt := "You are a helpful assistant. Reference material follows.\n" +
		strings.Repeat("The quick brown fox jumps over the lazy dog. ", 400)

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: systemPrompt},
		{Role: llms.RoleUser, Content: "In one sentence, what is this assistant for?"},
	}

	tracker := llms.NewCostTracker()

	// First call writes the prefix into the cache (cache_creation tokens).
	first, err := client.GenerateContent(ctx, messages, llms.WithCache())
	if err != nil {
		log.Fatalf("first call: %v", err)
	}
	tracker.Record(client.Provider(), client.Model(), first.Usage)
	fmt.Printf("call 1: %s\n", first.Content)
	printUsage("call 1", first.Usage)

	// Second call with the same prefix reads from the cache (cache_read tokens),
	// which is much cheaper than re-processing the whole system prompt.
	second, err := client.GenerateContent(ctx, messages, llms.WithCache())
	if err != nil {
		log.Fatalf("second call: %v", err)
	}
	tracker.Record(client.Provider(), client.Model(), second.Usage)
	printUsage("call 2", second.Usage)

	fmt.Printf("\nestimated total cost: %s\n", llms.FormatCost(tracker.GetTotalCost()))

	// Explicit per-message breakpoints give finer control (Anthropic): cache the
	// prefix up to and including a marked message.
	_ = []llms.Message{
		{Role: llms.RoleSystem, Content: systemPrompt},
		{
			Role:         llms.RoleUser,
			Content:      "Here is a long document to analyze...",
			CacheControl: &llms.CacheControl{TTL: time.Hour}, // 1-hour cache tier
		},
		{Role: llms.RoleUser, Content: "Summarize it."},
	}
}

func printUsage(label string, u llms.Usage) {
	fmt.Printf("%s usage: prompt=%d completion=%d cache_read=%d cache_creation=%d\n",
		label, u.PromptTokens, u.CompletionTokens, u.CacheReadTokens, u.CacheCreationTokens)
}
