// Example: Cost tracking
//
// This example demonstrates how to track token usage and estimate costs
// across multiple requests and providers.
//
// Run with: go run ./examples/cost-tracking
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pricing modes are pure estimation against the built-in tables, so this runs
	// without any provider credentials.
	demoPricingModes()

	baseClient := createClient()
	if baseClient == nil {
		cancel()
		log.Fatal("No API key found. Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY")
	}

	fmt.Printf("Using: %s (%s)\n\n", baseClient.Provider(), baseClient.Model())

	// Example 1: Basic cost tracking
	fmt.Println("=== Basic Cost Tracking ===")

	// Create a cost tracker
	tracker := llms.NewCostTracker()

	// Wrap the client with cost middleware
	client := llms.NewCostMiddleware(baseClient, tracker)

	// Make some requests
	prompts := []string{
		"What is the capital of France?",
		"Explain quantum computing in one sentence.",
		"Write a haiku about programming.",
	}

	for i, prompt := range prompts {
		fmt.Printf("Request %d: %s\n", i+1, prompt)
		resp, err := client.Call(ctx, prompt)
		if err != nil {
			log.Printf("Request failed: %v", err)
			continue
		}
		fmt.Printf("Response: %s\n\n", resp)
	}

	// Check accumulated costs
	fmt.Println("=== Cost Summary ===")
	promptTokens, completionTokens := tracker.GetTotalTokens()
	fmt.Printf("Total prompt tokens: %d\n", promptTokens)
	fmt.Printf("Total completion tokens: %d\n", completionTokens)
	fmt.Printf("Total requests: %d\n", tracker.GetTotalRequests())
	fmt.Printf("Estimated total cost: %s\n\n", llms.FormatCost(tracker.GetTotalCost()))

	// Example 2: Per-model breakdown
	fmt.Println("=== Per-Model Breakdown ===")
	for _, usage := range tracker.Report() {
		fmt.Printf("%s/%s:\n", usage.Provider, usage.Model)
		fmt.Printf("  Requests: %d\n", usage.Requests)
		fmt.Printf("  Prompt tokens: %d\n", usage.PromptTokens)
		fmt.Printf("  Completion tokens: %d\n", usage.CompletionTokens)
		fmt.Printf("  Estimated cost: %s\n", llms.FormatCost(usage.EstimatedCost))
		fmt.Printf("  First used: %s\n", usage.FirstUsed.Format(time.RFC3339))
		fmt.Printf("  Last used: %s\n\n", usage.LastUsed.Format(time.RFC3339))
	}

	// Example 3: Get specific model usage
	fmt.Println("=== Specific Model Usage ===")
	modelUsage := tracker.GetUsage(baseClient.Provider(), baseClient.Model())
	if modelUsage != nil {
		fmt.Printf("Usage for %s:\n", baseClient.Model())
		fmt.Printf("  Total tokens: %d\n\n", modelUsage.PromptTokens+modelUsage.CompletionTokens)
	}

	// Example 4: Streaming with cost tracking
	fmt.Println("=== Streaming with Cost Tracking ===")
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Count from 1 to 5, one number per line."},
	}

	stream, err := client.Stream(ctx, messages)
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}

	fmt.Print("Streaming response: ")
	for chunk := range stream {
		if chunk.Error != nil {
			log.Printf("Stream error: %v", chunk.Error)
			break
		}
		if !chunk.Done {
			fmt.Print(chunk.Content)
		}
	}
	fmt.Println()

	// Check updated costs after streaming
	fmt.Printf("\nUpdated total cost: %s\n\n", llms.FormatCost(tracker.GetTotalCost()))

	// Example 5: Custom pricing
	fmt.Println("=== Custom Pricing ===")
	customPricing := map[string]llms.Pricing{
		"custom:my-model": {
			Input:  1.00,
			Output: 2.00,
		},
	}

	customTracker := llms.NewCostTracker(customPricing)

	// You can also update pricing dynamically
	customTracker.SetPricing(llms.ProviderOpenAI, "gpt-4o-mini", llms.Pricing{
		Input:  0.20, // Custom rate
		Output: 0.80,
	})

	// Check if pricing exists
	pricing, exists := customTracker.GetPricing(llms.ProviderOpenAI, "gpt-4o-mini")
	if exists {
		fmt.Printf("Custom pricing for gpt-4o-mini:\n")
		fmt.Printf("  Prompt: $%.2f per 1M tokens\n", pricing.Input)
		fmt.Printf("  Completion: $%.2f per 1M tokens\n\n", pricing.Output)
	}

	// Example 6: Cost estimation without tracking
	fmt.Println("=== One-off Cost Estimation ===")
	usage := llms.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
	}

	estimatedCost, _ := llms.EstimateCost(llms.ProviderOpenAI, "gpt-4o", usage)
	fmt.Printf("Estimated cost for 1000 prompt + 500 completion tokens on GPT-4o:\n")
	fmt.Printf("  %s\n\n", llms.FormatCost(estimatedCost))

	// Example 7: Reset tracking
	fmt.Println("=== Reset Tracking ===")
	fmt.Printf("Before reset - Total requests: %d\n", tracker.GetTotalRequests())
	tracker.Reset()
	fmt.Printf("After reset - Total requests: %d\n", tracker.GetTotalRequests())
}

// demoPricingModes shows how billing lanes change the estimate. The mode is
// accounting only — it does not route the request. Send the request to the lane
// with the provider's own mechanism (OpenAI's service_tier, Anthropic's Batches
// API), then set the matching mode so the recorded cost is right.
func demoPricingModes() {
	fmt.Println("\n=== Pricing modes ===")

	// Deliberately below OpenAI's 272K long-context threshold so every lane is
	// compared on its short-context card. Above the threshold the standard card
	// tiers up but the batch/flex cards do not (their long-context columns are not
	// yet carried), which would make the comparison misleading rather than wrong.
	usage := llms.Usage{PromptTokens: 100_000, CompletionTokens: 100_000}
	for _, mode := range []llms.PricingMode{
		llms.PricingModeStandard,
		llms.PricingModeBatch,
		llms.PricingModeFlex,
		llms.PricingModeFast,
	} {
		label := string(mode)
		if mode == llms.PricingModeStandard {
			label = "standard"
		}
		cost, known := llms.EstimateCostMode(llms.ProviderOpenAI, "gpt-5.6-sol", usage, mode)
		note := ""
		if !known {
			// No published card for this lane: priced at standard rates, and the
			// boolean says so rather than implying a discount that does not exist.
			note = "  (no published rate for this mode; shown at standard rates)"
		}
		fmt.Printf("gpt-5.6-sol %-8s %s%s\n", label, llms.FormatCost(cost), note)
	}

	// Per-call: tag the request so the tracker books it in the right lane.
	tracker := llms.NewCostTracker()
	tracker.RecordMode(llms.ProviderOpenAI, "gpt-5.6-sol", usage, llms.PricingModeBatch)
	tracker.RecordMode(llms.ProviderOpenAI, "gpt-5.6-sol", usage, llms.PricingModeStandard)
	for mode, cost := range tracker.GetModeCosts() {
		label := string(mode)
		if mode == llms.PricingModeStandard {
			label = "standard"
		}
		fmt.Printf("booked %-8s %s\n", label, llms.FormatCost(cost))
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
