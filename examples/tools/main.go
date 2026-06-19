// Example: Function/Tool calling
//
// This example demonstrates how to use function calling (tools) with LLMs.
// The model can request to call functions, and you provide the results
// back to continue the conversation.
//
// Run with: go run ./examples/tools
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/providers/openai"
)

// Define the tools/functions the model can call.
var weatherTool = llms.NewFunctionTool(
	"get_weather",
	"Get the current weather for a location",
	map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "The city and state, e.g. San Francisco, CA",
			},
			"unit": map[string]any{
				"type":        "string",
				"enum":        []string{"celsius", "fahrenheit"},
				"description": "Temperature unit",
			},
		},
		"required": []string{"location"},
	},
)

var calculatorTool = llms.NewFunctionTool(
	"calculator",
	"Perform basic arithmetic operations",
	map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "subtract", "multiply", "divide"},
				"description": "The arithmetic operation to perform",
			},
			"a": map[string]any{
				"type":        "number",
				"description": "First operand",
			},
			"b": map[string]any{
				"type":        "number",
				"description": "Second operand",
			},
		},
		"required": []string{"operation", "a", "b"},
	},
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

	// Example 1: Weather tool
	fmt.Println("=== Weather Tool Example ===")
	runToolExample(ctx, client, "What's the weather like in Tokyo?", []llms.Tool{weatherTool})

	// Example 2: Calculator tool
	fmt.Println("\n=== Calculator Tool Example ===")
	runToolExample(ctx, client, "What is 42 multiplied by 17?", []llms.Tool{calculatorTool})

	// Example 3: Multiple tools available
	fmt.Println("\n=== Multiple Tools Example ===")
	runToolExample(ctx, client,
		"I'm planning a trip to Paris. What's the weather there? Also, if a hotel costs 150 euros per night for 5 nights, what's the total?",
		[]llms.Tool{weatherTool, calculatorTool})
}

func runToolExample(ctx context.Context, client llms.LLM, prompt string, tools []llms.Tool) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: prompt},
	}

	fmt.Printf("User: %s\n\n", prompt)

	// Loop to handle tool calls
	for i := 0; i < 5; i++ { // Max 5 iterations to prevent infinite loops
		resp, err := client.GenerateContent(ctx, messages,
			llms.WithTools(tools),
		)
		if err != nil {
			log.Fatalf("GenerateContent failed: %v", err)
		}

		// Check if the model wants to call tools
		if len(resp.ToolCalls) > 0 {
			fmt.Printf("Model requests %d tool call(s):\n", len(resp.ToolCalls))

			// Add assistant message with tool calls
			messages = append(messages, llms.Message{
				Role:      llms.RoleAssistant,
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			// Process each tool call
			for _, tc := range resp.ToolCalls {
				fmt.Printf("  - %s(%s)\n", tc.Function.Name, tc.Function.Arguments)

				// Execute the tool and get result
				result := executeTool(tc.Function.Name, tc.Function.Arguments)
				fmt.Printf("    Result: %s\n", result)

				// Add tool result to messages
				messages = append(messages, llms.Message{
					Role:       llms.RoleTool,
					Content:    result,
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
				})
			}
			fmt.Println()
		} else {
			// No more tool calls, we have the final response
			fmt.Printf("Assistant: %s\n", resp.Content)
			break
		}
	}
}

// executeTool simulates executing a tool and returning results.
func executeTool(name, arguments string) string {
	switch name {
	case "get_weather":
		var args struct {
			Location string `json:"location"`
			Unit     string `json:"unit"`
		}
		_ = json.Unmarshal([]byte(arguments), &args)

		unit := args.Unit
		if unit == "" {
			unit = "celsius"
		}

		// Simulated weather data
		temp := 22
		if unit == "fahrenheit" {
			temp = 72
		}
		return fmt.Sprintf(`{"temperature": %d, "unit": "%s", "condition": "sunny", "humidity": 45}`,
			temp, unit)

	case "calculator":
		var args struct {
			Operation string  `json:"operation"`
			A         float64 `json:"a"`
			B         float64 `json:"b"`
		}
		_ = json.Unmarshal([]byte(arguments), &args)

		var result float64
		switch args.Operation {
		case "add":
			result = args.A + args.B
		case "subtract":
			result = args.A - args.B
		case "multiply":
			result = args.A * args.B
		case "divide":
			if args.B != 0 {
				result = args.A / args.B
			}
		}
		return fmt.Sprintf(`{"result": %g}`, result)

	default:
		return `{"error": "unknown tool"}`
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
