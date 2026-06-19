// Example: Streaming responses
//
// This example demonstrates how to stream LLM responses in real-time
// using Go channels, providing a better user experience for long responses.
//
// Run with: go run ./examples/streaming
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := createClient()
	if client == nil {
		cancel()
		log.Fatal("No API key found. Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY")
	}

	fmt.Printf("Using: %s (%s)\n\n", client.Provider(), client.Model())

	// Example 1: Basic streaming
	fmt.Println("=== Basic Streaming ===")
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Write a haiku about programming."},
	}

	stream, err := client.Stream(ctx, messages)
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}

	fmt.Print("Response: ")
	for chunk := range stream {
		if chunk.Error != nil {
			log.Fatalf("Stream error: %v", chunk.Error)
		}
		if chunk.Done {
			fmt.Printf("\n\n[Done - %s, %d tokens]\n",
				chunk.FinishReason, chunk.Usage.TotalTokens)
			break
		}
		fmt.Print(chunk.Content)
	}

	// Example 2: Streaming with options
	fmt.Println("\n=== Streaming with Options ===")
	messages = []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a creative storyteller."},
		{Role: llms.RoleUser, Content: "Tell me a very short story about a robot (2-3 sentences)."},
	}

	stream, err = client.Stream(ctx, messages,
		llms.WithTemperature(0.8),
		llms.WithMaxTokens(200),
	)
	if err != nil {
		log.Fatalf("Stream with options failed: %v", err)
	}

	fmt.Print("Response: ")
	var totalContent string
	for chunk := range stream {
		if chunk.Error != nil {
			log.Fatalf("Stream error: %v", chunk.Error)
		}
		if !chunk.Done {
			fmt.Print(chunk.Content)
			totalContent += chunk.Content
		} else {
			fmt.Printf("\n\n[Received %d characters]\n", len(totalContent))
		}
	}

	// Example 3: Handling stream with timeout
	fmt.Println("\n=== Streaming with Custom Timeout ===")
	shortCtx, shortCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shortCancel()

	messages = []llms.Message{
		{Role: llms.RoleUser, Content: "Count from 1 to 5, one number per line."},
	}

	stream, err = client.Stream(shortCtx, messages)
	if err != nil {
		log.Fatalf("Stream with timeout failed: %v", err)
	}

	fmt.Println("Response:")
	for chunk := range stream {
		if chunk.Error != nil {
			// Could be context deadline exceeded
			fmt.Printf("\nStream ended: %v\n", chunk.Error)
			break
		}
		if chunk.Done {
			fmt.Println("\n[Complete]")
			break
		}
		fmt.Print(chunk.Content)
	}
}

func createClient() llms.LLM {
	if os.Getenv("OPENAI_API_KEY") != "" {
		client, err := openai.New()
		if err == nil {
			return client
		}
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		client, err := anthropic.New()
		if err == nil {
			return client
		}
	}
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		client, err := gemini.New()
		if err == nil {
			return client
		}
	}
	return nil
}
