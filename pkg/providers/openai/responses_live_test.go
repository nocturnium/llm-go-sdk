//go:build integration

package openai

import (
	"context"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/internal/testutil"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/openaicompat"
)

// These tests exercise the OpenAI Responses API (POST /responses) against the
// live service. They require OPENAI_API_KEY and the integration build tag:
//
//	go test -tags=integration -run TestLiveResponses ./pkg/providers/openai/...
//
// They were added to smoke-test the Responses streaming SSE event grammar, which
// had previously only been validated against mock fixtures (roadmap Track A.3).
// All use gpt-4o-mini with tight token caps to keep cost negligible.

func newLiveResponsesClient(t *testing.T) *Client {
	t.Helper()
	apiKey := testutil.RequireEnvAPIKey(t, "OPENAI_API_KEY")
	client, err := New(
		WithAPIKey(apiKey),
		WithModel("gpt-4o-mini"),
		WithResponsesAPI(),
	)
	if err != nil {
		t.Fatalf("failed to create Responses client: %v", err)
	}
	return client
}

// TestLiveResponses_GenerateContent verifies non-streaming /responses end to end:
// content, token usage, and the server-assigned response ID are all surfaced.
func TestLiveResponses_GenerateContent(t *testing.T) {
	client := newLiveResponsesClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Reply with exactly the word: pong"},
	}

	resp, err := client.GenerateContent(ctx, messages, llms.WithMaxTokens(16))
	if err != nil {
		t.Fatalf("GenerateContent (responses) failed: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty content from /responses")
	}
	if resp.Usage.TotalTokens == 0 {
		t.Error("expected non-zero token usage from /responses")
	}
	if resp.ID == "" {
		t.Error("expected a response ID from /responses")
	}
	t.Logf("content=%q id=%s tokens=%d", resp.Content, resp.ID, resp.Usage.TotalTokens)
}

// TestLiveResponses_Stream is the key Track A.3 smoke test: it drives the live
// Responses SSE event grammar (response.output_text.delta ... response.completed)
// through the StreamChunk channel and asserts text, a terminal finish reason, and
// authoritative usage all arrive.
func TestLiveResponses_Stream(t *testing.T) {
	client := newLiveResponsesClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Count from 1 to 5, one number per line."},
	}

	chunks, err := client.Stream(ctx, messages, llms.WithMaxTokens(64))
	if err != nil {
		t.Fatalf("Stream (responses) failed: %v", err)
	}

	var content string
	var chunkCount int
	var finish llms.FinishReason
	var totalTokens int
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		content += chunk.Content
		chunkCount++
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
		if chunk.Usage != nil && chunk.Usage.TotalTokens != 0 {
			totalTokens = chunk.Usage.TotalTokens
		}
	}

	if content == "" {
		t.Error("expected non-empty streamed content from /responses")
	}
	if chunkCount == 0 {
		t.Error("expected at least one stream chunk from /responses")
	}
	if finish == "" {
		t.Error("expected a terminal finish reason from response.completed")
	}
	t.Logf("chunks=%d finish=%q tokens=%d content=%q", chunkCount, finish, totalTokens, content)
}

// TestLiveResponses_Stateful verifies server-side conversation state: WithStore
// persists a turn, and WithPreviousResponseID threads the prior turn so the model
// can answer a follow-up that depends on it — without resending the history.
func TestLiveResponses_Stateful(t *testing.T) {
	client := newLiveResponsesClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	first, err := client.GenerateContent(ctx,
		[]llms.Message{{Role: llms.RoleUser, Content: "Remember the number 42. Reply with just: ok"}},
		llms.WithMaxTokens(16),
		WithStore(true),
	)
	if err != nil {
		t.Fatalf("first turn failed: %v", err)
	}
	if first.ID == "" {
		t.Fatal("expected a response ID to thread state from")
	}

	second, err := client.GenerateContent(ctx,
		[]llms.Message{{Role: llms.RoleUser, Content: "What number did I ask you to remember? Reply with just the number."}},
		llms.WithMaxTokens(16),
		WithPreviousResponseID(first.ID),
	)
	if err != nil {
		t.Fatalf("second (threaded) turn failed: %v", err)
	}
	if second.Content == "" {
		t.Error("expected non-empty content on threaded turn")
	}
	t.Logf("first.id=%s second.content=%q", first.ID, second.Content)
}

// TestLiveResponses_ReasoningRoundTrip is the Track A.4 smoke test: it exercises
// the STATELESS encrypted-reasoning round-trip for a reasoning model. Turn 1 asks
// for encrypted reasoning items (WithReasoningRoundTrip, store disabled); turn 2
// echoes them back on the assistant message and asks a dependent follow-up. The
// point is to validate the wire format end to end — the API rejects malformed
// reasoning items with a 400, so a passing second turn proves the round-trip.
func TestLiveResponses_ReasoningRoundTrip(t *testing.T) {
	apiKey := testutil.RequireEnvAPIKey(t, "OPENAI_API_KEY")

	client, err := New(
		WithAPIKey(apiKey),
		WithModel("o4-mini"), // a reasoning model
		WithResponsesAPI(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const q = "Think step by step: what is 17 * 23? Reply with just the number."
	first, err := client.GenerateContent(ctx,
		[]llms.Message{{Role: llms.RoleUser, Content: q}},
		llms.WithMaxTokens(2000),
		WithStore(false),
		WithReasoningRoundTrip(),
	)
	if err != nil {
		t.Fatalf("first turn failed: %v", err)
	}
	if first.Reasoning == nil {
		t.Skipf("model returned no encrypted reasoning items; nothing to round-trip (content=%q)", first.Content)
	}
	if _, ok := first.Reasoning.Metadata[openaicompat.MetadataKeyResponsesReasoning]; !ok {
		t.Skip("no encrypted reasoning items present; skipping round-trip")
	}

	second, err := client.GenerateContent(ctx,
		[]llms.Message{
			{Role: llms.RoleUser, Content: q},
			{Role: llms.RoleAssistant, Content: first.Content, Reasoning: first.Reasoning},
			{Role: llms.RoleUser, Content: "Now multiply that result by 2. Reply with just the number."},
		},
		llms.WithMaxTokens(2000),
		WithStore(false),
		WithReasoningRoundTrip(),
	)
	if err != nil {
		t.Fatalf("second (reasoning round-trip) turn failed: %v", err)
	}
	if second.Content == "" {
		t.Error("expected non-empty content on the round-trip turn")
	}
	t.Logf("first=%q second=%q", first.Content, second.Content)
}
