// Example: Resilience patterns
//
// This example demonstrates how to use circuit breakers and retry logic
// to build resilient LLM applications that handle failures gracefully.
//
// Run with: go run ./examples/resilience
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/middleware/resilience"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/openai"
)

func main() {
	ctx := context.Background()

	baseClient := createClient()
	if baseClient == nil {
		log.Fatal("No API key found. Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY")
	}

	fmt.Printf("Using: %s (%s)\n\n", baseClient.Provider(), baseClient.Model())

	// Example 1: Basic resilient client with defaults
	fmt.Println("=== Basic Resilient Client ===")
	resilientClient := resilience.NewResilientClient(baseClient)

	resp, err := resilientClient.Call(ctx, "Say 'Hello, resilient world!' and nothing else.")
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n\n", resp)
	}

	// Example 2: Custom retry configuration
	fmt.Println("=== Custom Retry Configuration ===")
	customRetry := &resilience.RetryConfig{
		MaxAttempts:   5,                      // Try up to 5 times
		InitialDelay:  500 * time.Millisecond, // Start with 500ms delay
		MaxDelay:      10 * time.Second,       // Cap at 10 seconds
		BackoffFactor: 2.0,                    // Double delay each retry
		Jitter:        0.2,                    // Add 20% randomness
		ShouldRetry:   resilience.DefaultShouldRetry,
	}

	retryClient := resilience.NewResilientClient(baseClient,
		resilience.WithRetryConfig(customRetry),
		resilience.WithOnRetry(func(attempt int, err error, delay time.Duration) {
			fmt.Printf("  Retry attempt %d after %v (error: %v)\n", attempt, delay, err)
		}),
	)

	resp, err = retryClient.Call(ctx, "Respond with just 'OK'")
	if err != nil {
		log.Printf("Request failed after retries: %v", err)
	} else {
		fmt.Printf("Response: %s\n\n", resp)
	}

	// Example 3: Circuit breaker with custom settings
	fmt.Println("=== Circuit Breaker ===")
	cb := resilience.NewCircuitBreaker(
		resilience.WithMaxFailures(3),              // Open after 3 failures
		resilience.WithResetTimeout(5*time.Second), // Try again after 5s
		resilience.WithHalfOpenMax(2),              // Allow 2 test requests in half-open
		resilience.WithOnStateChange(func(from, to resilience.CircuitState) {
			fmt.Printf("  Circuit breaker: %s -> %s\n", from, to)
		}),
	)

	cbClient := resilience.NewResilientClient(baseClient,
		resilience.WithCircuitBreaker(cb),
		resilience.WithMaxRetries(2),
	)

	// Make a few requests to demonstrate circuit breaker behavior
	for i := 1; i <= 3; i++ {
		fmt.Printf("Request %d: ", i)
		resp, err := cbClient.Call(ctx, fmt.Sprintf("Say 'Request %d received'", i))
		if err != nil {
			fmt.Printf("failed - %v\n", err)
		} else {
			fmt.Printf("success - %s\n", resp)
		}
	}

	// Check circuit breaker state
	fmt.Printf("\nCircuit state: %s\n\n", cb.State())

	// Example 4: Rate limiting with resilience
	fmt.Println("=== Combined Rate Limiting + Resilience ===")

	// First wrap with rate limiting
	rateLimitedClient := resilience.NewRateLimitedClient(baseClient,
		resilience.WithRequestsPerMinute(30),
		resilience.WithBlocking(true),
		resilience.WithWaitTimeout(5*time.Second),
	)

	// Then wrap with resilience
	fullClient := resilience.NewResilientClient(rateLimitedClient,
		resilience.WithMaxRetries(3),
	)

	resp, err = fullClient.Call(ctx, "Demonstrate the full middleware stack.")
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n\n", resp)
	}

	// Example 5: Quick retry settings
	fmt.Println("=== Quick Retry Settings ===")
	quickClient := resilience.NewResilientClient(baseClient,
		resilience.WithMaxRetries(2),             // Shorthand for MaxAttempts=3
		resilience.WithRetryDelay(1*time.Second), // Shorthand for InitialDelay
	)

	resp, err = quickClient.Call(ctx, "Quick response please.")
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		fmt.Printf("Response: %s\n", resp)
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
