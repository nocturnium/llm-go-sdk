package llms_test

import (
	"context"
	"fmt"
	"log"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
)

// These examples are compiled (so they stay in sync with the API) but not
// executed: they have no "// Output:" line and would otherwise require live
// provider credentials. They exist to document the canonical call shapes that
// godoc surfaces next to each symbol.

// Example shows the shortest path: construct a provider and send one prompt.
func Example() {
	client, err := openai.New() // reads OPENAI_API_KEY from the environment
	if err != nil {
		log.Fatal(err)
	}

	answer, err := llms.Call(context.Background(), client, "Say hello in one word.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer)
}

// ExampleCall demonstrates a single-prompt call with options.
func ExampleCall() {
	client, err := openai.New()
	if err != nil {
		log.Fatal(err)
	}

	answer, err := llms.Call(context.Background(), client,
		"Name the capital of France.",
		llms.WithTemperature(0),
		llms.WithMaxTokens(16),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer)
}

// Example_messages shows multi-turn control via GenerateContent.
func Example_messages() {
	client, err := openai.New()
	if err != nil {
		log.Fatal(err)
	}

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a terse assistant."},
		{Role: llms.RoleUser, Content: "What is Go?"},
	}
	resp, err := client.GenerateContent(context.Background(), messages,
		llms.WithTemperature(0.7),
		llms.WithMaxTokens(256),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Content)
}

// Example_streaming drains a stream to its final text with StreamText.
func Example_streaming() {
	client, err := openai.New()
	if err != nil {
		log.Fatal(err)
	}

	messages := []llms.Message{{Role: llms.RoleUser, Content: "Count to three."}}
	chunks, err := client.Stream(context.Background(), messages)
	if err != nil {
		log.Fatal(err)
	}

	text, err := llms.StreamText(chunks)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(text)
}

// ExampleRunTools wires a typed tool handler into the agent loop.
func ExampleRunTools() {
	client, err := openai.New()
	if err != nil {
		log.Fatal(err)
	}

	type weatherArgs struct {
		City string `json:"city"`
	}

	registry := llms.NewToolRegistry()
	tool := llms.NewFunctionTool("get_weather", "Get the weather for a city", weatherArgs{})
	llms.RegisterFunc(registry, tool, func(_ context.Context, args weatherArgs) (any, error) {
		return fmt.Sprintf("It is sunny in %s.", args.City), nil
	})

	messages := []llms.Message{{Role: llms.RoleUser, Content: "What's the weather in Paris?"}}
	resp, _, err := llms.RunTools(context.Background(), client, messages, registry)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Content)
}

// ExampleGenerateTyped unmarshals the model's response into a Go struct using
// an automatically generated JSON Schema.
func ExampleGenerateTyped() {
	client, err := openai.New()
	if err != nil {
		log.Fatal(err)
	}

	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	messages := []llms.Message{{Role: llms.RoleUser, Content: "Invent a person."}}
	person, _, err := llms.GenerateTyped[Person](context.Background(), client, messages)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s is %d\n", person.Name, person.Age)
}

// ExampleCostTracker records usage and reports the estimated cost.
func ExampleCostTracker() {
	client, err := openai.New()
	if err != nil {
		log.Fatal(err)
	}

	tracker := llms.NewCostTracker()
	messages := []llms.Message{{Role: llms.RoleUser, Content: "Hello!"}}
	resp, err := client.GenerateContent(context.Background(), messages)
	if err != nil {
		log.Fatal(err)
	}

	cost, known := tracker.Record(client.Provider(), client.Model(), resp.Usage)
	if known {
		fmt.Printf("request cost: $%.6f\n", cost)
	}
}

// ExampleNewFromEnv selects the provider at runtime from LLM_PROVIDER. This
// requires blank-importing the provider(s) (or pkg/providers/all) so they
// register themselves; see the package docs.
func ExampleNewFromEnv() {
	client, err := llms.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	answer, err := llms.Call(context.Background(), client, "Hello!")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer)
}
