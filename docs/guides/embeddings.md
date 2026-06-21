# Embeddings & Reranking

Embeddings turn text into dense numeric vectors so you can measure semantic
similarity, build retrieval-augmented generation (RAG) pipelines, cluster
documents, or power semantic search. Reranking takes a query plus a candidate
set of documents and returns them re-scored and re-ordered by relevance.

This guide covers both, using only the current public API.

!!! note "Vectors are `[]float32`"
    Every embedding vector in this SDK is a Go `[]float32`. The package-level
    helpers (`llms.EmbedQuery`, `llms.EmbedDocuments`) return `[]float32` and
    `[][]float32` respectively — no boxing, no `interface{}`.

## The `Embedder` interface

Not every provider supports embeddings, so embedding is a *capability*, not part
of the core `llms.LLM` interface. A provider that can embed implements the
single-method `Embedder` interface:

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string, options ...EmbedOption) (*EmbeddingResponse, error)
}
```

The response carries one vector per input text, plus token usage:

```go
type EmbeddingResponse struct {
    Embeddings []Embedding    // one per input, in input order
    Model      string         // model that produced the vectors
    Usage      EmbeddingUsage // PromptTokens, TotalTokens
}

type Embedding struct {
    Index  int       // index of the corresponding input text
    Vector []float32 // the embedding vector
    Object string    // usually "embedding"
}
```

## Getting an `Embedder` from a client

You construct a provider client the usual way, then ask whether it supports
embeddings with `llms.AsEmbedder`. This keeps your code provider-agnostic: the
same call site works whether the underlying client can embed or not.

```go
client, err := openai.New(
    openai.WithEmbeddingModel("text-embedding-3-small"),
)
if err != nil {
    log.Fatal(err)
}

embedder, ok := llms.AsEmbedder(client)
if !ok {
    log.Fatal("this provider does not support embeddings")
}
```

`llms.SupportsEmbeddings(client)` returns the same boolean if you only need to
check without keeping the typed `Embedder`.

!!! tip "Set the embedding model at construction"
    Use the provider's `WithEmbeddingModel(...)` option (available on all
    OpenAI-compatible providers, plus `gemini`, `ollama`, `llamacpp`, and
    `infinity`) so every embed call uses it by default. You can still override
    per call with `llms.WithEmbedModel(...)`.

## The package-level helpers

Rather than calling `Embed` directly and unpacking the response, most code uses
the two helpers, which return raw vectors:

```go
func EmbedQuery(ctx context.Context, e Embedder, text string, options ...EmbedOption) ([]float32, error)
func EmbedDocuments(ctx context.Context, e Embedder, texts []string, options ...EmbedOption) ([][]float32, error)
```

- **`EmbedQuery`** embeds one string and returns its single vector.
- **`EmbedDocuments`** embeds a batch and returns vectors in input order.

```go
queryVec, err := llms.EmbedQuery(ctx, embedder, "What is machine learning?")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("dimensions: %d\n", len(queryVec))

docVecs, err := llms.EmbedDocuments(ctx, embedder, []string{
    "Machine learning is a subset of artificial intelligence.",
    "The weather today is sunny and warm.",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("embedded %d documents\n", len(docVecs))
```

When you need token usage or the model name, call `Embed` directly and read the
response fields:

```go
resp, err := embedder.Embed(ctx, []string{"hello", "world"})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("model=%s tokens=%d vectors=%d\n",
    resp.Model, resp.Usage.TotalTokens, len(resp.Embeddings))
```

## Embed options

Pass `EmbedOption` values to any of `Embed`, `EmbedQuery`, or `EmbedDocuments`:

| Option | Effect |
| --- | --- |
| `llms.WithEmbedModel(model string)` | Override the model for this request. |
| `llms.WithDimensions(n int)` | Request reduced output dimensions (only some models, e.g. `text-embedding-3-*`). |
| `llms.WithEncodingFormat(format string)` | Encoding format, e.g. `"float"` (default) or `"base64"`. |
| `llms.WithEmbedUser(user string)` | Optional end-user identifier. |
| `llms.WithTaskType(taskType string)` | Task hint for Gemini embeddings. |

For Gemini, the task type accepts these constants:

```go
llms.TaskTypeRetrievalQuery    // "RETRIEVAL_QUERY"
llms.TaskTypeRetrievalDocument // "RETRIEVAL_DOCUMENT"
llms.TaskTypeSemantic          // "SEMANTIC_SIMILARITY"
llms.TaskTypeClassification    // "CLASSIFICATION"
llms.TaskTypeClustering        // "CLUSTERING"
```

```go
// Reduce dimensionality (where supported) and tag the user.
resp, err := embedder.Embed(ctx, []string{"text to embed"},
    llms.WithDimensions(256),
    llms.WithEmbedUser("user-42"),
)
```

!!! warning "Not every model supports dimension reduction"
    `WithDimensions` only takes effect on models that allow it. On models that
    don't, the option is ignored or the request errors — handle the error and
    fall back to the model's native size.

## Which providers support embeddings

These providers implement `Embedder` and can be used with `llms.AsEmbedder`:

| Provider | Constructor | Default embedding model |
| --- | --- | --- |
| openai | `openai.New(...)` | `text-embedding-3-small` |
| azure | `azure.New(...)` | (deployment-defined) |
| gemini | `gemini.New(...)` | `text-embedding-004` |
| mistral | `mistral.New(...)` | `mistral-embed` |
| ollama | `ollama.New(...)` | `nomic-embed-text` |
| llamacpp | `llamacpp.New(...)` | (model-dependent — set explicitly) |
| fireworks | `fireworks.New(...)` | (set explicitly) |
| togetherai | `togetherai.New(...)` | (set explicitly) |
| synthetic | `synthetic.New(...)` | (set explicitly) |
| infinity | `infinity.New(...)` | `michaelfeil/bge-small-en-v1.5` |

!!! note "Capability, not guarantee"
    OpenAI-compatible providers (groq, deepseek, perplexity, cerebras, etc.)
    satisfy `Embedder` via BaseProvider even when they have no embedding model,
    so `llms.AsEmbedder` alone does not prove a provider can embed. The reliable
    check is configuring an embedding model and handling
    `llms.ErrEmbeddingModelRequired` from `Embed` / `EmbedQuery` /
    `EmbedDocuments`. Native providers like anthropic may genuinely not
    implement `Embedder`.

## Reranking

Reranking is exposed through the `Reranker` interface. In this SDK it is
provided by the **infinity** provider (a high-throughput embeddings/reranking
server, commonly run locally or behind RunPod).

```go
type Reranker interface {
    Rerank(ctx context.Context, query string, documents []string, options ...RerankOption) (*RerankResponse, error)
}
```

Detect it with `llms.AsReranker` (or `llms.SupportsReranking`). Results come back
sorted by relevance score, highest first:

```go
type RerankResponse struct {
    Results []RerankResult // sorted by Score, descending
    Model   string
    Usage   RerankUsage
}

type RerankResult struct {
    Index    int     // original index in the input slice
    Score    float64 // relevance score (higher = more relevant)
    Document string  // populated only when WithReturnDocuments(true)
}
```

Rerank options:

| Option | Effect |
| --- | --- |
| `llms.WithRerankModel(model string)` | Override the rerank model for this request. |
| `llms.WithTopN(n int)` | Return only the top `n` results. |
| `llms.WithReturnDocuments(include bool)` | Include the original document text in each result. |

```go
client, err := infinity.New(
    infinity.WithBaseURL("http://localhost:7997/v1"),
    infinity.WithRerankModel("mixedbread-ai/mxbai-rerank-xsmall-v1"),
)
if err != nil {
    log.Fatal(err)
}

reranker, ok := llms.AsReranker(client)
if !ok {
    log.Fatal("provider does not support reranking")
}

resp, err := reranker.Rerank(ctx,
    "What is machine learning?",
    []string{
        "ML is a subset of AI.",
        "The weather is nice today.",
        "Neural networks learn from data.",
    },
    llms.WithTopN(2),
    llms.WithReturnDocuments(true),
)
if err != nil {
    log.Fatal(err)
}

for _, r := range resp.Results {
    fmt.Printf("%.4f  (orig #%d)  %s\n", r.Score, r.Index, r.Document)
}
```

!!! tip "Infinity runs locally by default"
    The `infinity` provider defaults to `http://localhost:7997/v1` and relaxes
    SSRF protection (private IPs and plain HTTP) out of the box, so a local
    Infinity server works with no extra flags. Point it elsewhere with
    `infinity.WithBaseURL(...)`. The default rerank model is
    `mixedbread-ai/mxbai-rerank-xsmall-v1`.

## End-to-end example: semantic search

This complete program embeds a small corpus, embeds a query, and ranks the
documents by cosine similarity. It selects whichever embedding provider has
credentials available.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "math"
    "os"
    "time"

    llms "github.com/nocturnium/llm-go-sdk/v4"
    "github.com/nocturnium/llm-go-sdk/v4/pkg/providers/gemini"
    "github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    embedder := pickEmbedder()
    if embedder == nil {
        log.Fatal("no embedding provider configured; set OPENAI_API_KEY or GEMINI_API_KEY")
    }

    documents := []string{
        "Machine learning is a subset of artificial intelligence.",
        "Deep learning uses neural networks with many layers.",
        "Computer vision enables machines to interpret images.",
        "The weather today is sunny and warm.",
    }

    // Embed the corpus in one batch.
    docVecs, err := llms.EmbedDocuments(ctx, embedder, documents)
    if err != nil {
        log.Fatalf("EmbedDocuments: %v", err)
    }

    // Embed the query.
    query := "How do computers understand pictures?"
    queryVec, err := llms.EmbedQuery(ctx, embedder, query)
    if err != nil {
        log.Fatalf("EmbedQuery: %v", err)
    }

    fmt.Printf("Query: %q\n\n", query)
    for i, dv := range docVecs {
        fmt.Printf("  %.4f  %s\n", cosineSimilarity(queryVec, dv), documents[i])
    }
}

// pickEmbedder returns the first embedding-capable client with credentials.
func pickEmbedder() llms.Embedder {
    if os.Getenv("OPENAI_API_KEY") != "" {
        if c, err := openai.New(openai.WithEmbeddingModel("text-embedding-3-small")); err == nil {
            if e, ok := llms.AsEmbedder(c); ok {
                return e
            }
        }
    }
    if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
        if c, err := gemini.New(); err == nil {
            if e, ok := llms.AsEmbedder(c); ok {
                return e
            }
        }
    }
    return nil
}

// cosineSimilarity returns the cosine similarity of two equal-length vectors.
func cosineSimilarity(a, b []float32) float64 {
    if len(a) != len(b) {
        return 0
    }
    var dot, normA, normB float64
    for i := range a {
        av, bv := float64(a[i]), float64(b[i])
        dot += av * bv
        normA += av * av
        normB += bv * bv
    }
    if normA == 0 || normB == 0 {
        return 0
    }
    return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
```

!!! note "Runnable in the repo"
    A maintained version of this example lives at
    [`examples/embeddings/main.go`](https://github.com/nocturnium/llm-go-sdk/blob/main/examples/embeddings/main.go).
    Run it with `go run ./examples/embeddings`.

## Errors to handle

| Sentinel | Meaning |
| --- | --- |
| `llms.ErrEmptyInput` | The input text (or every item in the batch) was empty. |
| `llms.ErrEmbeddingModelRequired` | No embedding model was set via `WithEmbeddingModel` or `WithEmbedModel`. |

You can validate a batch before sending it with
`llms.ValidateEmbedInput(texts []string) error`, which returns
`llms.ErrEmptyInput` for an empty slice or any empty element.
