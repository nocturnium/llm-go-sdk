// Package cloudflare provides a client for Cloudflare Workers AI.
//
// Chat and embeddings use Workers AI's OpenAI-compatible routes through
// openaicompat.BaseProvider. Decision models use the native /ai/run route,
// because they have no OpenAI-compatible form; this is the only native path
// besides the model catalog.
//
// # Authentication
//
// Set CLOUDFLARE_API_TOKEN (an API token with Workers AI permission) and
// CLOUDFLARE_ACCOUNT_ID, or use WithAPIKey and WithAccountID. LLM_API_KEY is the
// generic fallback for the token.
//
// # Default Models
//
// Chat: "@cf/openai/gpt-oss-120b" (WithModel). Embeddings: "@cf/baai/bge-m3"
// (WithEmbeddingModel). Decisions: "@cf/cloudflare/clef-flash"
// (WithDecisionModel). All three were live-verified on 2026-11-14.
//
// # Supported Features
//
//   - Chat completions, streaming and tools (model dependent)
//   - Embeddings
//   - Decisions (llms.Decider): Clef, Clef-flash and Clef-omni answer noul,
//     choice and score questions; Clairic extracts text, date, number,
//     list and estimate fields. Images are sent as embedded data URIs.
//   - Model listing from the account catalog, with context window and vision
//     flags read from the catalog rather than hardcoded
//
// # Example
//
//	client, err := cloudflare.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	text, err := llms.Call(ctx, client, "Hello!")
//
//	resp, err := client.Decide(ctx, llms.DecisionRequest{
//	    State: "Checkout is failing for every customer.",
//	    Questions: map[string]llms.DecisionQuestion{
//	        "urgent": {Kind: llms.DecisionKindNoul, Instructions: "Is this urgent?"},
//	    },
//	})
package cloudflare
