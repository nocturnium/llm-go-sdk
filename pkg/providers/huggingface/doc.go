// Package huggingface provides an embeddings client for HuggingFace Inference
// Endpoints.
//
// HuggingFace serves embedding/feature-extraction models with Text Embeddings
// Inference (TEI), which exposes an OpenAI-compatible embeddings route at
// POST <endpoint>/v1/embeddings. This client targets that route, reusing the
// SDK's OpenAI-compatible HTTP client (SSRF protection, retries, error mapping).
//
// Because Inference Endpoints are per-deployment URLs, an endpoint is required
// (WithEndpoint); the token is the Bearer credential, taken from WithAPIKey or
// the HF_TOKEN / HUGGINGFACE_API_KEY environment variables.
//
// The client implements llms.Embedder and is embeddings-only: it does not
// implement the chat llms.LLM interface and is not registered in the by-name
// provider registry (construct it directly with New).
package huggingface
