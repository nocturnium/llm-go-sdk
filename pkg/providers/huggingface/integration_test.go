//go:build integration

package huggingface

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveEmbed exercises a real HuggingFace Inference Endpoint. It reads the
// endpoint URL and token from EMBEDDING_ENDPOINT and EMBEDDING_TOKEN (falling back
// to HF_TOKEN), and skips when they are not set.
//
//	EMBEDDING_ENDPOINT=https://xxxx.endpoints.huggingface.cloud \
//	EMBEDDING_TOKEN=hf_... \
//	GOWORK=off go test -tags integration ./pkg/providers/huggingface/ -run TestLiveEmbed -v
func TestLiveEmbed(t *testing.T) {
	endpoint := os.Getenv("EMBEDDING_ENDPOINT")
	token := os.Getenv("EMBEDDING_TOKEN")
	if token == "" {
		token = os.Getenv("HF_TOKEN")
	}
	if endpoint == "" || token == "" {
		t.Skip("set EMBEDDING_ENDPOINT and EMBEDDING_TOKEN (or HF_TOKEN) to run the live test")
	}

	client, err := New(WithEndpoint(endpoint), WithAPIKey(token), WithTimeout(60*time.Second))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()

	resp, err := client.Embed(ctx, []string{"hello world", "embeddings over HTTP"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 2 {
		t.Fatalf("got %d embeddings, want 2", len(resp.Embeddings))
	}
	for i, e := range resp.Embeddings {
		if len(e.Vector) == 0 {
			t.Fatalf("embedding %d is empty", i)
		}
	}
	t.Logf("live embed ok: %d vectors, dim=%d", len(resp.Embeddings), len(resp.Embeddings[0].Vector))

	vec, err := client.EmbedQuery(ctx, "single query")
	if err != nil || len(vec) == 0 {
		t.Fatalf("EmbedQuery: vec_len=%d err=%v", len(vec), err)
	}
}
