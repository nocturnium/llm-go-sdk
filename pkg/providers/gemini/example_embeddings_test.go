package gemini_test

import (
	"context"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/gemini"
)

// Example_embeddings is a compile guard for the embeddings snippet in the package
// doc. EmbedDocuments is a package-level llms function — not a method on Embedder —
// so a regression back to embedder.EmbedDocuments(...) would fail to build here.
// It has no // Output: line, so it is compiled but never executed (no network).
func Example_embeddings() {
	client, err := gemini.New()
	if err != nil {
		return
	}
	embedder, ok := llms.AsEmbedder(client)
	if !ok {
		return
	}
	texts := []string{"text1", "text2"}
	vectors, err := llms.EmbedDocuments(context.Background(), embedder, texts,
		llms.WithTaskType(llms.TaskTypeRetrievalDocument),
	)
	_, _ = vectors, err
}
