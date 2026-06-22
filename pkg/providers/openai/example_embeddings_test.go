package openai_test

import (
	"context"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
)

// Example_embeddings is a compile guard for the embeddings snippet in the package
// doc. EmbedDocuments is a package-level llms function — not a method on Embedder —
// so a regression back to embedder.EmbedDocuments(...) would fail to build here.
// It has no // Output: line, so it is compiled but never executed (no network).
func Example_embeddings() {
	client, err := openai.New(openai.WithAPIKey("sk-..."))
	if err != nil {
		return
	}
	embedder, ok := llms.AsEmbedder(client)
	if !ok {
		return
	}
	vectors, err := llms.EmbedDocuments(context.Background(), embedder, []string{"text1", "text2"})
	_, _ = vectors, err
}
