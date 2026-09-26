package llmadk

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Keys the bridge sets in LLMResponse.CustomMetadata. Numbers come back as
// float64 after a session is stored and reloaded as JSON.
const (
	MetadataProvider            = "llmgo.provider"
	MetadataModel               = "llmgo.model"
	MetadataResponseID          = "llmgo.response_id"
	MetadataServiceTier         = "llmgo.service_tier"
	MetadataCostUSD             = "llmgo.cost_usd"
	MetadataCacheCreationTokens = "llmgo.cache_creation_tokens"
	MetadataAdjustments         = "llmgo.adjustments"
	MetadataEnvelopesDropped    = "llmgo.envelopes_dropped"
	MetadataOrphansDropped      = "llmgo.orphans_dropped"
)

// idPrefixADK marks IDs ADK assigns itself; ADK strips them from history, so a
// call must never carry one or its result could not be paired.
const idPrefixADK = "adk-"

// minKeptIDLength is the shortest provider ID the bridge keeps. Shorter IDs
// (llama.cpp's "0", a server's "call_1") repeat across turns.
const minKeptIDLength = 9

// responseTranslator carries what building a response needs from the request.
type responseTranslator struct {
	tr *translation
	// fallbackProvider and fallbackModel stamp reasoning that arrived without a
	// stamp; empty when the wrapped LLM is a router whose serving provider is
	// unknown.
	fallbackProvider llms.Provider
	fallbackModel    string
}

// convert builds the ADK response for a complete turn.
func (rt *responseTranslator) convert(resp *llms.Response) (*model.LLMResponse, error) {
	if rt.tr.structured {
		resolveStructured(resp)
	}
	rt.stamp(resp.Reasoning, resp.Provider, resp.Model)
	rt.assignIDs(resp.ToolCalls)

	var parts []*genai.Part
	// A turn that is only hidden thought (thinking that ran out of tokens) gets
	// no Thought part: ADK re-calls the model after a thought-only turn, up to
	// ten times, and native Gemini returns no part in that case either. With no
	// answer and no call there is nothing to replay.
	thoughtOnly := resp.Content == "" && len(resp.ToolCalls) == 0
	if !rt.tr.hideThoughts || !thoughtOnly {
		if part := rt.thoughtPart(resp.Reasoning); part != nil {
			parts = append(parts, part)
		}
	}
	if resp.Content != "" {
		parts = append(parts, &genai.Part{Text: resp.Content})
	}
	for i, tc := range resp.ToolCalls {
		part, err := functionCallPart(tc)
		if err != nil {
			return nil, err
		}
		if i == 0 && part.ThoughtSignature == nil {
			// Gemini 3 requires a signature on the first function call of each
			// step, and ADK's own Gemini model sends the stored ThoughtSignature
			// as it is. Google's placeholder for calls Gemini did not make keeps
			// the session usable if the agent moves to that model; every other
			// provider drops it as Gemini's.
			part.ThoughtSignature = []byte(geminiSkipValidator)
		}
		parts = append(parts, part)
	}

	out := &model.LLMResponse{
		UsageMetadata:  usageMetadata(resp.Usage),
		CustomMetadata: rt.customMetadata(resp),
		ModelVersion:   firstNonEmpty(resp.ModelVersion, resp.Model, rt.fallbackModel),
		TurnComplete:   true,
	}
	finish := finishReason(resp.FinishReason, resp.Content != "" || len(resp.ToolCalls) > 0)
	out.FinishReason = finish
	if len(parts) > 0 {
		out.Content = &genai.Content{Role: string(genai.RoleModel), Parts: parts}
	} else if finish == genai.FinishReasonStop {
		out.Content = &genai.Content{Role: string(genai.RoleModel)}
	} else {
		// No answer and no stop, a truncated or filtered turn: ADK's own Gemini
		// model reports the finish reason as the error code.
		out.ErrorCode = string(finish)
	}
	if len(resp.SearchResults) > 0 {
		out.GroundingMetadata = groundingMetadata(resp.SearchResults)
	}
	return out, nil
}

// stamp records who produced reasoning that arrived unstamped, so replay can
// be decided on a later turn. Providers in this module stamp their own; this
// covers third-party LLM implementations.
func (rt *responseTranslator) stamp(rc *llms.ReasoningContent, servedBy llms.Provider, servedModel string) {
	if rc == nil || rc.Provider != "" {
		return
	}
	switch {
	case servedBy != "":
		rc.Provider, rc.Model = servedBy, servedModel
	case rt.fallbackProvider != "":
		rc.Provider, rc.Model = rt.fallbackProvider, rt.fallbackModel
	}
}

// thoughtPart renders reasoning as a Thought part whose ThoughtSignature
// carries everything needed to replay it. With thoughts hidden the part's text
// is empty and the envelope holds the text instead.
func (rt *responseTranslator) thoughtPart(rc *llms.ReasoningContent) *genai.Part {
	if rc == nil || (rc.Content == "" && rc.Signature == "" && len(rc.Metadata) == 0) {
		return nil
	}
	part := &genai.Part{Thought: true}
	// Gemini needs only its signature back, never the text, so its reasoning
	// is stored as Gemini issued it: raw signature bytes that ADK's own Gemini
	// model can also replay, and no envelope, which Gemini answers "Corrupted
	// thought signature". With thoughts hidden and no signature (Gemini 3 signs
	// the function call instead) there is nothing to keep.
	if rc.Provider == llms.ProviderGemini && len(rc.Metadata) == 0 {
		raw, _ := geminiSignatureBytes(rc.Signature)
		if rt.tr.hideThoughts && raw == nil {
			return nil
		}
		part.ThoughtSignature = raw
		if !rt.tr.hideThoughts {
			part.Text = rc.Content
		}
		return part
	}
	if rt.tr.hideThoughts {
		setEnvelope(part, thoughtEnvelope(rc, true))
	} else {
		part.Text = rc.Content
		setEnvelope(part, thoughtEnvelope(rc, false))
	}
	return part
}

// assignIDs makes every call's ID unique across the session. ADK pairs each
// call with its result by ID over the whole history, so an ID that repeats an
// earlier call's (a server's "call_0" every turn) would attach one call's
// result to another. A provider ID is kept when it cannot collide; anything
// else gets a fresh random one, which is what ADK then stores.
func (rt *responseTranslator) assignIDs(calls []llms.ToolCall) {
	seen := map[string]bool{}
	for i := range calls {
		id := calls[i].ID
		keep := len(id) >= minKeptIDLength && !strings.HasPrefix(id, idPrefixADK) &&
			!rt.tr.historyIDs[id] && !seen[id]
		for !keep {
			id = llms.NewToolCallID()
			keep = !rt.tr.historyIDs[id] && !seen[id]
		}
		calls[i].ID = id
		seen[id] = true
	}
}

// functionCallPart renders a tool call. Its arguments must be a JSON object,
// which is what ADK passes to the tool.
func functionCallPart(tc llms.ToolCall) (*genai.Part, error) {
	if tc.Function == nil {
		return nil, fmt.Errorf("%w: tool call %q has no function", ErrFunctionCallArgs, tc.ID)
	}
	args := map[string]any{}
	if raw := strings.TrimSpace(tc.Function.Arguments); raw != "" && raw != jsonNull {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrFunctionCallArgs, tc.Function.Name, err)
		}
		if args == nil {
			args = map[string]any{}
		}
	}
	part := &genai.Part{FunctionCall: &genai.FunctionCall{ID: tc.ID, Name: tc.Function.Name, Args: args}}
	attachCallSignature(part, tc)
	return part, nil
}

// finishReason maps an llm-go-sdk finish reason onto genai's. A tool-call
// finish is STOP, as Gemini reports it, and a response with content but no
// reason finished normally.
func finishReason(r llms.FinishReason, hasOutput bool) genai.FinishReason {
	switch r {
	case llms.FinishReasonStop, llms.FinishReasonToolCalls:
		return genai.FinishReasonStop
	case llms.FinishReasonLength:
		return genai.FinishReasonMaxTokens
	case llms.FinishReasonContentFilter:
		return genai.FinishReasonSafety
	case "":
		if hasOutput {
			return genai.FinishReasonStop
		}
		return genai.FinishReasonOther
	default:
		return genai.FinishReasonOther
	}
}

// usageMetadata maps usage onto genai's accounting, which counts cached tokens
// inside the prompt and thought tokens outside the candidates. llm-go-sdk
// counts the reverse, so both are converted; ADK's history compaction triggers
// on the prompt count and would never fire on a count missing a cache hit.
func usageMetadata(u llms.Usage) *genai.GenerateContentResponseUsageMetadata {
	if u == (llms.Usage{}) {
		return nil
	}
	prompt := u.PromptTokens + u.CacheReadTokens + u.CacheCreationTokens
	candidates := u.CompletionTokens - u.ReasoningTokens
	if candidates < 0 {
		candidates = 0
	}
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:        clampInt32(prompt),
		CandidatesTokenCount:    clampInt32(candidates),
		ThoughtsTokenCount:      clampInt32(u.ReasoningTokens),
		CachedContentTokenCount: clampInt32(u.CacheReadTokens),
		TotalTokenCount:         clampInt32(prompt + candidates + u.ReasoningTokens),
	}
}

// clampInt32 narrows a token count to genai's int32, saturating rather than
// wrapping.
func clampInt32(n int) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < 0:
		return 0
	default:
		return int32(n)
	}
}

func (rt *responseTranslator) customMetadata(resp *llms.Response) map[string]any {
	md := map[string]any{}
	set := func(k string, v any, ok bool) {
		if ok {
			md[k] = v
		}
	}
	set(MetadataProvider, string(resp.Provider), resp.Provider != "")
	set(MetadataModel, resp.Model, resp.Model != "")
	set(MetadataResponseID, resp.ID, resp.ID != "")
	set(MetadataServiceTier, resp.ServiceTier, resp.ServiceTier != "")
	if resp.Usage.Cost != nil {
		md[MetadataCostUSD] = *resp.Usage.Cost
	}
	set(MetadataCacheCreationTokens, resp.Usage.CacheCreationTokens, resp.Usage.CacheCreationTokens > 0)
	set(MetadataAdjustments, resp.Adjustments, len(resp.Adjustments) > 0)
	set(MetadataEnvelopesDropped, rt.tr.droppedEnvelopes, rt.tr.droppedEnvelopes > 0)
	set(MetadataOrphansDropped, rt.tr.droppedOrphans, rt.tr.droppedOrphans > 0)
	if len(md) == 0 {
		return nil
	}
	return md
}

// groundingMetadata reports web search results as grounding chunks.
func groundingMetadata(results []llms.SearchResult) *genai.GroundingMetadata {
	gm := &genai.GroundingMetadata{}
	for _, r := range results {
		gm.GroundingChunks = append(gm.GroundingChunks, &genai.GroundingChunk{
			Web: &genai.GroundingChunkWeb{URI: r.URL, Title: r.Title},
		})
	}
	return gm
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
