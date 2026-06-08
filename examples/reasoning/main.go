// Example: Reasoning ("thinking") models
//
// Demonstrates the cross-provider reasoning controls: requesting a reasoning
// effort or token budget, reading the model's reasoning output from
// Response.Reasoning, and inspecting reasoning token usage. The same options
// work across OpenAI (reasoning_effort), Anthropic (extended thinking budget),
// Gemini, and Z.AI/Qwen-style models.
//
// Run with: OPENAI_API_KEY=... go run ./examples/reasoning
// or:        ANTHROPIC_API_KEY=... go run ./examples/reasoning
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/pkg/providers/openai"
)

const prompt = "A bat and a ball cost $1.10 in total. The bat costs $1.00 more than the ball. How much does the ball cost? Think it through."

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	switch {
	case os.Getenv("ANTHROPIC_API_KEY") != "":
		runAnthropic(ctx)
	case os.Getenv("OPENAI_API_KEY") != "":
		runOpenAI(ctx)
	default:
		log.Fatal("set ANTHROPIC_API_KEY or OPENAI_API_KEY to run this example")
	}
}

func runAnthropic(ctx context.Context) {
	client, err := anthropic.New(anthropic.WithModel("claude-sonnet-4-20250514"))
	if err != nil {
		log.Fatalf("anthropic: %v", err)
	}

	// A token budget enables Anthropic extended thinking; the SDK also bumps
	// max_tokens above the budget automatically.
	resp, err := client.GenerateContent(ctx,
		[]llms.Message{{Role: llms.RoleUser, Content: prompt}},
		llms.WithReasoningBudget(4096),
	)
	if err != nil {
		log.Fatalf("generate: %v", err)
	}
	report(resp)
}

func runOpenAI(ctx context.Context) {
	client, err := openai.New(openai.WithModel("o4-mini"))
	if err != nil {
		log.Fatalf("openai: %v", err)
	}

	// Effort maps to OpenAI's reasoning_effort parameter.
	resp, err := client.GenerateContent(ctx,
		[]llms.Message{{Role: llms.RoleUser, Content: prompt}},
		llms.WithReasoningEffort(llms.ReasoningEffortHigh),
	)
	if err != nil {
		log.Fatalf("generate: %v", err)
	}
	report(resp)
}

func report(resp *llms.Response) {
	if r := resp.ReasoningText(); r != "" {
		fmt.Printf("--- reasoning ---\n%s\n\n", r)
	}
	fmt.Printf("--- answer ---\n%s\n\n", resp.Content)
	fmt.Printf("tokens: prompt=%d completion=%d reasoning=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.ReasoningTokens)
}
