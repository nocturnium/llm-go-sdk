// Example: Using the new package structure
//
// This example demonstrates the new organized import style.
// The root package (llms) re-exports all common types, so you only need
// to import the root package and the provider you want to use.
//
// Both old and new provider import paths work identically:
//
//	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"     // old
//	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"  // new
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	client, err := openai.New(openai.WithAPIKey(apiKey))
	if err != nil {
		log.Fatal(err)
	}

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a helpful assistant."},
		{Role: llms.RoleUser, Content: "What is the capital of France?"},
	}

	resp, err := client.GenerateContent(ctx, messages,
		llms.WithTemperature(0.7),
		llms.WithMaxTokens(100),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Content)
}
