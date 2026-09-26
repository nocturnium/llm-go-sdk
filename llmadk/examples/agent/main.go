// Example: an ADK agent served by any llm-go-sdk provider.
//
// It builds a Google ADK agent with one tool and runs a turn on the provider
// named by LLM_PROVIDER (anthropic, openai, gemini, openrouter, ...; default
// anthropic), reading that provider's usual API key variable.
//
// Run with: go run ./examples/agent (from the llmadk directory)
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"

	"github.com/nocturnium/llm-go-sdk/llmadk"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	_ "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/all" // registers every provider with llms.New
)

type weatherArgs struct {
	City string `json:"city"`
}

func main() {
	provider := os.Getenv("LLM_PROVIDER")
	if provider == "" {
		provider = "anthropic"
	}
	llm, err := llms.New(provider, llms.Config{})
	if err != nil {
		log.Fatalf("provider %s: %v", provider, err)
	}
	model, err := llmadk.NewModel(llm)
	if err != nil {
		log.Fatal(err)
	}

	weather, err := functiontool.New(functiontool.Config{Name: "get_weather", Description: "Current weather for a city."},
		func(_ agent.Context, a weatherArgs) (map[string]any, error) {
			return map[string]any{"city": a.City, "forecast": "sunny", "celsius": 21}, nil
		})
	if err != nil {
		log.Fatal(err)
	}
	assistant, err := llmagent.New(llmagent.Config{
		Name:        "assistant",
		Model:       model,
		Instruction: "Answer weather questions with the get_weather tool.",
		Tools:       []tool.Tool{weather},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	sessions := session.InMemoryService()
	if _, err := sessions.Create(ctx, &session.CreateRequest{AppName: "example", UserID: "user", SessionID: "s1"}); err != nil {
		log.Fatal(err)
	}
	r, err := runner.New(runner.Config{AppName: "example", Agent: assistant, SessionService: sessions})
	if err != nil {
		log.Fatal(err)
	}
	msg := genai.NewContentFromText("What's the weather in Lisbon?", genai.RoleUser)
	for event, err := range r.Run(ctx, "user", "s1", msg, agent.RunConfig{}) {
		if err != nil {
			log.Fatal(err)
		}
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			switch {
			case part.FunctionCall != nil:
				fmt.Printf("-> %s(%v)\n", part.FunctionCall.Name, part.FunctionCall.Args)
			case part.Text != "" && !part.Thought:
				fmt.Println(part.Text)
			}
		}
	}
}
