package llmadk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"

	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Reasoning a provider needs back travels with the part that carries it and
// is stored with the session. Gemini's own signatures go in the part's
// ThoughtSignature as the raw bytes Gemini issued, so a session stays usable by
// ADK's own Gemini model. Every other provider's reasoning goes in an envelope
// in the part's PartMetadata under partMetadataKey, never in ThoughtSignature:
// Gemini validates ThoughtSignature and answers "Corrupted thought signature"
// for bytes it did not issue, while it accepts and ignores PartMetadata. (ADK's
// Gemini model on Vertex AI refuses PartMetadata client-side, so a session
// holding another provider's reasoning cannot move to it.)
//
// The envelope is the magic prefix, a version byte, then JSON. It carries everything a provider
// needs to replay its reasoning (the signature, the thought text Anthropic
// requires alongside it, Anthropic's redacted_thinking blocks, OpenAI's
// encrypted reasoning items) and the provenance stamp that decides which
// provider may replay it.
//
// The format is persisted in users' session stores, so it is versioned: a
// change bumps envelopeVersion and keeps a decoder for the old one.
var envelopeMagic = []byte("llmgo")

const (
	envelopeVersion byte = 1
	// maxEnvelopeSize bounds what is decoded. With thoughts hidden the envelope
	// holds the whole thought text, which a large thinking budget makes
	// hundreds of kilobytes; anything past this is not an envelope we wrote.
	maxEnvelopeSize = 8 << 20
)

// geminiSkipValidator is the thought signature Google documents for function
// calls Gemini did not produce; Gemini 3 skips signature validation for them.
const geminiSkipValidator = "skip_thought_signature_validator"

// partMetadataKey is the PartMetadata key holding a base64 envelope.
const partMetadataKey = "llmgo.reasoning"

// Envelope kinds.
const (
	kindThought = "thought"
	kindCall    = "call"
)

type envelope struct {
	Kind      string         `json:"k"`
	Provider  llms.Provider  `json:"p,omitempty"`
	Model     string         `json:"m,omitempty"`
	Content   *string        `json:"c,omitempty"`
	Signature string         `json:"s,omitempty"`
	Metadata  map[string]any `json:"md,omitempty"`
	Tokens    int            `json:"t,omitempty"`
}

func encodeEnvelope(e envelope) []byte {
	body, err := json.Marshal(e)
	if err != nil {
		// Metadata comes from provider responses, which decoded from JSON and
		// so marshal back to it; a value that does not is dropped rather than
		// failing the turn.
		e.Metadata = nil
		body, _ = json.Marshal(e)
	}
	out := append([]byte(nil), envelopeMagic...)
	out = append(out, envelopeVersion)
	return append(out, body...)
}

// thoughtEnvelope encodes reasoning for a Thought part. The text is included
// only when the part will not show it, since the part's own Text holds it
// otherwise.
func thoughtEnvelope(rc *llms.ReasoningContent, includeText bool) []byte {
	e := envelope{
		Kind:      kindThought,
		Provider:  rc.Provider,
		Model:     rc.Model,
		Signature: rc.Signature,
		Metadata:  rc.Metadata,
		Tokens:    rc.Tokens,
	}
	if includeText {
		text := rc.Content
		e.Content = &text
	}
	return encodeEnvelope(e)
}

// attachCallSignature stores a tool call's signature on its FunctionCall part:
// Gemini's as raw ThoughtSignature bytes, anyone else's as an envelope in
// PartMetadata.
func attachCallSignature(part *genai.Part, tc llms.ToolCall) {
	if tc.Signature == "" {
		return
	}
	if tc.SignatureProvider == llms.ProviderGemini {
		if raw, ok := geminiSignatureBytes(tc.Signature); ok {
			part.ThoughtSignature = raw
			return
		}
	}
	setEnvelope(part, encodeEnvelope(envelope{Kind: kindCall, Provider: tc.SignatureProvider, Signature: tc.Signature}))
}

// setEnvelope stores an encoded envelope in a part's PartMetadata.
func setEnvelope(part *genai.Part, env []byte) {
	if part.PartMetadata == nil {
		part.PartMetadata = map[string]any{}
	}
	part.PartMetadata[partMetadataKey] = base64.StdEncoding.EncodeToString(env)
}

// partSignature reads what reasoning a part carries: an envelope in its
// PartMetadata, or else Gemini's raw signature in its ThoughtSignature.
func partSignature(p *genai.Part) (envelope, decodeResult) {
	if v, ok := p.PartMetadata[partMetadataKey]; ok {
		s, isString := v.(string)
		raw, err := base64.StdEncoding.DecodeString(s)
		if !isString || err != nil {
			return envelope{}, sigInvalid
		}
		return decodeEnvelope(raw)
	}
	return nativeSignature(p.ThoughtSignature)
}

// geminiSignatureBytes turns a Gemini thought signature, which llm-go-sdk
// carries in the base64 form Gemini's API uses on the wire, back into the raw
// bytes genai holds. Storing Gemini's signatures that way, rather than in an
// envelope, keeps a session usable by ADK's own Gemini model, which sends
// ThoughtSignature to Gemini unchanged and is answered "Corrupted thought
// signature" for anything Gemini did not issue. decodeSignature reads raw bytes
// back as a Gemini signature.
func geminiSignatureBytes(sig string) ([]byte, bool) {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || len(raw) == 0 || bytes.HasPrefix(raw, envelopeMagic) {
		return nil, false
	}
	return raw, true
}

// decodeResult says what a ThoughtSignature held.
type decodeResult int

const (
	sigNone     decodeResult = iota // empty
	sigEnvelope                     // an envelope the bridge wrote
	sigNative                       // bytes from ADK's native Gemini model
	sigInvalid                      // an envelope that is corrupt or oversized
)

// nativeSignature reads a ThoughtSignature, which only ever holds bytes Gemini
// issued (through the bridge or through ADK's own Gemini model). They are
// returned as a Gemini-stamped signature in the base64 form Gemini's API
// carries on the wire, so the SDK's Gemini provider can replay them and every
// other provider drops them.
func nativeSignature(b []byte) (envelope, decodeResult) {
	if len(b) == 0 {
		return envelope{}, sigNone
	}
	return envelope{
		Kind:      kindThought,
		Provider:  llms.ProviderGemini,
		Signature: base64.StdEncoding.EncodeToString(b),
	}, sigNative
}

// decodeEnvelope reads an envelope the bridge stored in PartMetadata. A
// corrupt or oversized one is reported as sigInvalid and ignored by the caller:
// a damaged session must not break the agent.
func decodeEnvelope(b []byte) (envelope, decodeResult) {
	if len(b) > maxEnvelopeSize || len(b) < len(envelopeMagic)+1 || !bytes.HasPrefix(b, envelopeMagic) {
		return envelope{}, sigInvalid
	}
	switch b[len(envelopeMagic)] {
	case envelopeVersion:
		var e envelope
		if err := json.Unmarshal(b[len(envelopeMagic)+1:], &e); err != nil {
			return envelope{}, sigInvalid
		}
		return e, sigEnvelope
	default:
		return envelope{}, sigInvalid
	}
}

// reasoning rebuilds the ReasoningContent an envelope carries. visibleText is
// the Thought part's own text, used when the envelope did not store the text.
func (e envelope) reasoning(visibleText string) *llms.ReasoningContent {
	text := visibleText
	if e.Content != nil {
		text = *e.Content
	}
	return &llms.ReasoningContent{
		Content:   text,
		Signature: e.Signature,
		Metadata:  e.Metadata,
		Tokens:    e.Tokens,
		Provider:  e.Provider,
		Model:     e.Model,
	}
}
