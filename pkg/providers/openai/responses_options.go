package openai

import llms "github.com/nocturnium/llm-go-sdk/v3"

// This file holds per-call (llms.CallOption) helpers for the Responses API.
// Note these return llms.CallOption and are passed to GenerateContent/Call/Stream,
// unlike the client-construction Option values (WithAPIKey, WithResponsesAPI, …).
// They require the client to have been built with WithResponsesAPI().

// WithPreviousResponseID chains this request onto a prior Responses-API response,
// continuing the server-side conversation state. Pass the ID returned on the
// previous response (Response.ID). No effect unless the client was built with
// WithResponsesAPI().
//
//	resp, _ := client.GenerateContent(ctx, msgs)
//	next, _ := client.GenerateContent(ctx, more, openai.WithPreviousResponseID(resp.ID))
func WithPreviousResponseID(id string) llms.CallOption {
	return llms.WithExtraBodyParam("previous_response_id", id)
}

// WithStore controls whether the Responses API persists this response server-side
// (so it can be referenced via WithPreviousResponseID on a later turn). No effect
// unless the client was built with WithResponsesAPI().
func WithStore(store bool) llms.CallOption {
	return llms.WithExtraBodyParam("store", store)
}

// WithReasoningRoundTrip requests encrypted reasoning items on a Responses-API
// turn so a reasoning model's thinking can be replayed on a stateless follow-up.
// The returned Response.Reasoning carries the items; copy that Response.Reasoning
// onto the assistant Message you append to the history and they are echoed back on
// the next request. Use this for o-series / gpt-5 reasoning models when you are NOT
// using server-side state (WithStore / WithPreviousResponseID) — e.g. zero-data-
// retention deployments. No effect unless the client was built with WithResponsesAPI().
func WithReasoningRoundTrip() llms.CallOption {
	return llms.WithExtraBodyParam("include_encrypted_reasoning", true)
}
