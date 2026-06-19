//go:build integration

package openai

import (
	"context"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/internal/testutil"
)

// These tests make real API calls and require OPENAI_API_KEY to be set.
// Run with: go test -tags=integration ./providers/openai/...

func TestLiveClient_Call(t *testing.T) {
	apiKey := testutil.RequireEnvAPIKey(t, "OPENAI_API_KEY")

	client, err := New(
		WithAPIKey(apiKey),
		WithModel("gpt-4o-mini"), // Use cheaper model for testing
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := client.Call(ctx, "Say 'hello' and nothing else.")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if response == "" {
		t.Error("expected non-empty response")
	}

	t.Logf("Response: %s", response)
}

func TestLiveClient_GenerateContent(t *testing.T) {
	apiKey := testutil.RequireEnvAPIKey(t, "OPENAI_API_KEY")

	client, err := New(
		WithAPIKey(apiKey),
		WithModel("gpt-4o-mini"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "What is 2+2? Reply with just the number."},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content == "" {
		t.Error("expected non-empty content")
	}

	if resp.Usage.TotalTokens == 0 {
		t.Error("expected non-zero token usage")
	}

	t.Logf("Response: %s (tokens: %d)", resp.Content, resp.Usage.TotalTokens)
}

func TestLiveClient_Stream(t *testing.T) {
	apiKey := testutil.RequireEnvAPIKey(t, "OPENAI_API_KEY")

	client, err := New(
		WithAPIKey(apiKey),
		WithModel("gpt-4o-mini"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Count from 1 to 5, one number per line."},
	}

	chunks, err := client.Stream(ctx, messages)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var content string
	var chunkCount int
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		content += chunk.Content
		chunkCount++
	}

	if content == "" {
		t.Error("expected non-empty content from stream")
	}

	if chunkCount == 0 {
		t.Error("expected at least one chunk")
	}

	t.Logf("Received %d chunks, content: %s", chunkCount, content)
}
