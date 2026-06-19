package llms

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type stubEmbedder struct {
	resp     *EmbeddingResponse
	err      error
	gotTexts []string
}

func (s *stubEmbedder) Embed(_ context.Context, texts []string, _ ...EmbedOption) (*EmbeddingResponse, error) {
	s.gotTexts = texts
	return s.resp, s.err
}

type stubReranker struct {
	resp *RerankResponse
	err  error
}

func (s *stubReranker) Rerank(_ context.Context, _ string, _ []string, _ ...RerankOption) (*RerankResponse, error) {
	return s.resp, s.err
}

func TestEmbedQuery(t *testing.T) {
	ctx := context.Background()

	e := &stubEmbedder{resp: &EmbeddingResponse{Embeddings: []Embedding{{Vector: []float32{1, 2, 3}}}}}
	vec, err := EmbedQuery(ctx, e, "hello")
	if err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if !reflect.DeepEqual(vec, []float32{1, 2, 3}) {
		t.Errorf("vector = %v, want [1 2 3]", vec)
	}
	if !reflect.DeepEqual(e.gotTexts, []string{"hello"}) {
		t.Errorf("embedder received %v, want [hello]", e.gotTexts)
	}

	// Empty response → ErrEmptyInput.
	empty := &stubEmbedder{resp: &EmbeddingResponse{}}
	if _, err := EmbedQuery(ctx, empty, "x"); !errors.Is(err, ErrEmptyInput) {
		t.Errorf("empty response error = %v, want ErrEmptyInput", err)
	}

	// Underlying error is propagated.
	boom := errors.New("boom")
	if _, err := EmbedQuery(ctx, &stubEmbedder{err: boom}, "x"); !errors.Is(err, boom) {
		t.Errorf("error = %v, want boom", err)
	}
}

func TestEmbedDocuments(t *testing.T) {
	ctx := context.Background()
	e := &stubEmbedder{resp: &EmbeddingResponse{Embeddings: []Embedding{
		{Vector: []float32{1, 1}},
		{Vector: []float32{2, 2}},
	}}}
	got, err := EmbedDocuments(ctx, e, []string{"a", "b"})
	if err != nil {
		t.Fatalf("EmbedDocuments: %v", err)
	}
	want := [][]float32{{1, 1}, {2, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	boom := errors.New("boom")
	if _, err := EmbedDocuments(ctx, &stubEmbedder{err: boom}, []string{"a"}); !errors.Is(err, boom) {
		t.Errorf("error = %v, want boom", err)
	}
}

func TestSupportsAndAsEmbedder(t *testing.T) {
	e := &stubEmbedder{}
	if !SupportsEmbeddings(e) {
		t.Error("SupportsEmbeddings(embedder) = false, want true")
	}
	if SupportsEmbeddings("not an embedder") {
		t.Error("SupportsEmbeddings(string) = true, want false")
	}
	if got, ok := AsEmbedder(e); !ok || got != e {
		t.Errorf("AsEmbedder(embedder) = %v,%v", got, ok)
	}
	if _, ok := AsEmbedder(42); ok {
		t.Error("AsEmbedder(int) ok = true, want false")
	}
}

func TestSupportsAndAsReranker(t *testing.T) {
	r := &stubReranker{}
	if !SupportsReranking(r) {
		t.Error("SupportsReranking(reranker) = false, want true")
	}
	if SupportsReranking("nope") {
		t.Error("SupportsReranking(string) = true, want false")
	}
	if got, ok := AsReranker(r); !ok || got != r {
		t.Errorf("AsReranker(reranker) = %v,%v", got, ok)
	}
	if _, ok := AsReranker(struct{}{}); ok {
		t.Error("AsReranker(struct{}) ok = true, want false")
	}
}

func TestApplyRerankOptions(t *testing.T) {
	// Defaults.
	def := ApplyRerankOptions()
	if def.Model != "" || def.TopN != 0 || def.ReturnDocuments {
		t.Errorf("defaults = %+v, want zero values", def)
	}

	// All options applied.
	opts := ApplyRerankOptions(
		WithRerankModel("rerank-v2"),
		WithTopN(5),
		WithReturnDocuments(true),
	)
	if opts.Model != "rerank-v2" || opts.TopN != 5 || !opts.ReturnDocuments {
		t.Errorf("opts = %+v", opts)
	}
}
