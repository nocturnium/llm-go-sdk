// Example: Fallback chains
//
// This example demonstrates how to set up automatic failover between
// multiple LLM providers for high availability.
//
// Run with: go run ./examples/fallback
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/middleware/resilience"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Collect available providers
	var clients []llms.LLM
	var providerNames []string

	if os.Getenv("OPENAI_API_KEY") != "" {
		if client, err := openai.New(); err == nil {
			clients = append(clients, client)
			providerNames = append(providerNames, "OpenAI")
		}
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		if client, err := anthropic.New(); err == nil {
			clients = append(clients, client)
			providerNames = append(providerNames, "Anthropic")
		}
	}
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		if client, err := gemini.New(); err == nil {
			clients = append(clients, client)
			providerNames = append(providerNames, "Gemini")
		}
	}

	if len(clients) == 0 {
		cancel()
		log.Fatal("No API keys found. Set at least one of: OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY")
	}

	fmt.Printf("Available providers: %v\n\n", providerNames)

	if len(clients) < 2 {
		fmt.Println("Note: Set multiple API keys to see fallback behavior")
		fmt.Println("      With one provider, requests go directly to it")
	}

	// Example 1: Basic fallback chain
	fmt.Println("=== Basic Fallback Chain ===")
	chain := resilience.NewFallbackChain(clients)

	resp, err := chain.Call(ctx, "What is 2+2? Reply with just the number.")
	if err != nil {
		log.Printf("All providers failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n", resp)
		fmt.Printf("Handled by: %s\n\n", chain.Provider())
	}

	// Example 2: Fallback with callbacks
	fmt.Println("=== Fallback with Monitoring ===")
	monitoredChain := resilience.NewFallbackChain(clients,
		resilience.WithOnFallback(func(_, _ int, from, to llms.LLM, err error) {
			fmt.Printf("  Falling back: %s -> %s (error: %v)\n",
				from.Provider(), to.Provider(), err)
		}),
		resilience.WithOnSuccess(func(idx int, client llms.LLM) {
			fmt.Printf("  Success with provider #%d: %s\n", idx, client.Provider())
		}),
	)

	resp, err = monitoredChain.Call(ctx, "Say 'Hello from fallback chain'")
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n\n", resp)
	}

	// Example 3: Different fallback selectors
	fmt.Println("=== Fallback Selectors ===")

	// Default: Falls back on rate limits, server errors, circuit open
	defaultChain := resilience.NewFallbackChain(clients,
		resilience.WithFallbackSelector(resilience.DefaultFallbackSelector{}),
	)
	fmt.Println("DefaultFallbackSelector: Falls back on 429, 5xx, circuit open")

	// Always: Falls back on any error
	alwaysChain := resilience.NewFallbackChain(clients,
		resilience.WithFallbackSelector(resilience.AlwaysFallbackSelector{}),
	)
	fmt.Println("AlwaysFallbackSelector: Falls back on any error")

	// Never: No fallback, fail fast
	neverChain := resilience.NewFallbackChain(clients,
		resilience.WithFallbackSelector(resilience.NeverFallbackSelector{}),
	)
	fmt.Println("NeverFallbackSelector: No fallback, fail immediately")

	// Use the default chain for demonstration
	resp, err = defaultChain.Call(ctx, "Quick test")
	if err == nil {
		fmt.Printf("\nTest response: %s\n\n", resp)
	}

	// Suppress unused variable warnings
	_ = alwaysChain
	_ = neverChain

	// Example 4: Weighted fallback (prioritization)
	if len(clients) >= 2 {
		fmt.Println("=== Weighted Fallback ===")
		weights := make([]int, len(clients))
		for i := range weights {
			weights[i] = 10 - i*3 // Higher priority for earlier providers
		}

		weightedChain, err := resilience.NewWeightedFallbackChain(clients, weights)
		if err != nil {
			log.Printf("Failed to create weighted chain: %v", err)
		} else {
			fmt.Printf("Weights: %v\n", weights)
			fmt.Printf("Order (highest weight first): ")
			for i, c := range weightedChain.Clients() {
				if i > 0 {
					fmt.Print(" -> ")
				}
				fmt.Print(c.Provider())
			}
			fmt.Println()

			resp, err = weightedChain.Call(ctx, "Weighted test")
			if err == nil {
				fmt.Printf("Response: %s\n\n", resp)
			}
		}
	}

	// Example 5: Health management
	fmt.Println("=== Health Management ===")
	healthChain := resilience.NewFallbackChain(clients)

	// Check health status
	for i := range clients {
		fmt.Printf("Provider %d (%s): healthy=%v\n",
			i, clients[i].Provider(), healthChain.IsClientHealthy(i))
	}

	// Manually mark a provider as unhealthy (e.g., based on external health check)
	if len(clients) > 0 {
		fmt.Println("\nMarking first provider as unhealthy...")
		healthChain.SetClientHealthy(0, false)

		resp, err = healthChain.Call(ctx, "Test with first provider unhealthy")
		if err == nil {
			fmt.Printf("Response (should skip unhealthy): %s\n", resp)
		}

		// Reset health
		healthChain.ResetHealth()
		fmt.Println("Health reset - all providers marked healthy again")
	}

	// Example 6: Dynamic client management
	fmt.Println("\n=== Dynamic Client Management ===")
	dynamicChain := resilience.NewFallbackChain(clients[:1]) // Start with one client

	fmt.Printf("Initial clients: %d\n", len(dynamicChain.Clients()))

	if len(clients) > 1 {
		dynamicChain.AddClient(clients[1])
		fmt.Printf("After adding client: %d\n", len(dynamicChain.Clients()))

		dynamicChain.RemoveClient(0)
		fmt.Printf("After removing client: %d\n", len(dynamicChain.Clients()))
	}
}
