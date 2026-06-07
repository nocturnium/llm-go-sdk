// Example: Basic chat completion with multiple providers
//
// This example demonstrates the simplest way to use the llms package
// for chat completions with different providers.
//
// Run with: go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try different providers based on available API keys
	client, providerName := createClient()
	if client == nil {
		cancel()
		log.Fatal("No API key found. Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY")
	}

	fmt.Printf("Using provider: %s (model: %s)\n\n", providerName, client.Model())

	// Example 1: Simple call with just a prompt
	fmt.Println("=== Simple Call ===")
	response, err := llms.Call(ctx, client, "What is the capital of France? Reply in one sentence.")
	if err != nil {
		log.Fatalf("Call failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", response)

	// Example 2: GenerateContent with messages for more control
	fmt.Println("=== With Messages ===")
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a helpful assistant that gives concise answers."},
		{Role: llms.RoleUser, Content: "What are the three primary colors?"},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		log.Fatalf("GenerateContent failed: %v", err)
	}

	fmt.Printf("Response: %s\n", resp.Content)
	fmt.Printf("Tokens: %d prompt, %d completion, %d total\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	fmt.Printf("Finish reason: %s\n\n", resp.FinishReason)

	// Example 3: Using call options
	fmt.Println("=== With Options ===")
	resp, err = client.GenerateContent(ctx, messages,
		llms.WithTemperature(0.3), // Lower temperature for more focused responses
		llms.WithMaxTokens(100),   // Limit response length
	)
	if err != nil {
		log.Fatalf("GenerateContent with options failed: %v", err)
	}
	fmt.Printf("Response: %s\n", resp.Content)

	// Example 4: Multi-turn conversation
	fmt.Println("\n=== Multi-turn Conversation ===")
	conversation := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a helpful math tutor."},
		{Role: llms.RoleUser, Content: "What is 2 + 2?"},
	}

	resp, err = client.GenerateContent(ctx, conversation)
	if err != nil {
		log.Fatalf("First turn failed: %v", err)
	}
	fmt.Printf("User: What is 2 + 2?\n")
	fmt.Printf("Assistant: %s\n", resp.Content)

	// Add assistant response and continue
	conversation = append(conversation,
		llms.Message{Role: llms.RoleAssistant, Content: resp.Content},
		llms.Message{Role: llms.RoleUser, Content: "Now multiply that by 3"},
	)

	resp, err = client.GenerateContent(ctx, conversation)
	if err != nil {
		log.Fatalf("Second turn failed: %v", err)
	}
	fmt.Printf("User: Now multiply that by 3\n")
	fmt.Printf("Assistant: %s\n", resp.Content)
}

// createClient tries to create a client with available API keys.
func createClient() (llms.LLM, string) {
	// Try OpenAI first
	if os.Getenv("OPENAI_API_KEY") != "" {
		client, err := openai.New()
		if err == nil {
			return client, "OpenAI"
		}
	}

	// Try Anthropic
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		client, err := anthropic.New()
		if err == nil {
			return client, "Anthropic"
		}
	}

	// Try Gemini
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		client, err := gemini.New()
		if err == nil {
			return client, "Gemini"
		}
	}

	return nil, ""
}
