package llmadk

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// translation is an ADK request rendered for llm-go-sdk.
type translation struct {
	messages    []llms.Message
	callOptions []llms.CallOption
	// structured is set when the bridge offered the response schema as a
	// set_model_response tool (see structured.go).
	structured bool
	// hideThoughts is set when thought text must not be shown as a thought part.
	hideThoughts bool
	// timeout bounds the call when the request's HTTPOptions set one.
	timeout time.Duration
	// historyIDs holds every tool-call ID already in the request, which a new
	// call's ID must not repeat.
	historyIDs map[string]bool
	// droppedEnvelopes counts reasoning envelopes that could not be decoded.
	droppedEnvelopes int
	// droppedOrphans counts tool results that answered no call.
	droppedOrphans int
	// hasTools and hasImages record what the request asks of the provider, for
	// the capability checks.
	hasTools, hasImages, hasSchema bool
}

// translator carries the per-request state of a translation.
type translator struct {
	opts *options
	tr   *translation
	// pendingCalls holds, per function name, the IDs synthesized for calls that
	// arrived without one, so their ID-less responses can be paired in order.
	pendingCalls map[string][]string
}

func (m *Model) translate(req *model.LLMRequest) (*translation, error) {
	if req == nil {
		return nil, ErrRequestNil
	}
	if len(req.Contents) == 0 {
		return nil, ErrNoContents
	}
	t := &translator{
		opts:         m.opts,
		tr:           &translation{hideThoughts: true, historyIDs: map[string]bool{}},
		pendingCalls: map[string][]string{},
	}
	collectHistoryIDs(req.Contents, t.tr.historyIDs)

	cfg := req.Config
	if cfg == nil {
		cfg = &genai.GenerateContentConfig{}
	}
	if system := systemText(cfg.SystemInstruction); system != "" {
		t.tr.messages = append(t.tr.messages, llms.Message{Role: llms.RoleSystem, Content: system})
	}
	for i, content := range req.Contents {
		if err := t.content(i, content); err != nil {
			return nil, err
		}
	}
	t.tr.messages, t.tr.droppedOrphans = repairToolResults(t.tr.messages)

	if err := t.config(cfg); err != nil {
		return nil, err
	}
	if req.Model != "" && req.Model != m.name {
		t.tr.callOptions = append(t.tr.callOptions, llms.WithModel(req.Model))
	}
	return t.tr, nil
}

// systemText joins the text parts of a system instruction.
func systemText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var texts []string
	for _, p := range c.Parts {
		if p == nil || p.Text == "" {
			continue
		}
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n\n")
}

// collectHistoryIDs records every call and response ID in contents.
func collectHistoryIDs(contents []*genai.Content, ids map[string]bool) {
	for _, c := range contents {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			switch {
			case p == nil:
			case p.FunctionCall != nil && p.FunctionCall.ID != "":
				ids[p.FunctionCall.ID] = true
			case p.FunctionResponse != nil && p.FunctionResponse.ID != "":
				ids[p.FunctionResponse.ID] = true
			}
		}
	}
}

// unsupportedPart is the error for a part the bridge cannot carry.
func unsupportedPart(index int, what string) error {
	return fmt.Errorf("%w: contents[%d] %s", ErrUnsupportedPart, index, what)
}

// content translates one genai Content into messages.
func (t *translator) content(index int, c *genai.Content) error {
	if c == nil {
		return nil
	}
	if c.Role == string(genai.RoleModel) {
		return t.modelContent(index, c)
	}
	return t.userContent(index, c)
}

// hardPartField names a part field the bridge cannot carry, if the part has one.
func hardPartField(p *genai.Part) string {
	switch {
	case p.ExecutableCode != nil:
		return "ExecutableCode"
	case p.CodeExecutionResult != nil:
		return "CodeExecutionResult"
	case p.VideoMetadata != nil:
		return "VideoMetadata"
	case p.ToolCall != nil:
		return "ToolCall"
	case p.ToolResponse != nil:
		return "ToolResponse"
	case p.AudioTranscription != nil:
		return "AudioTranscription"
	}
	return ""
}

func isZeroPart(p *genai.Part) bool {
	return p == nil || reflect.ValueOf(*p).IsZero()
}

// modelContent translates a model turn into one assistant message. ID-less
// responses pair only with ID-less calls of the model turn just before them,
// so a call left open earlier (a long-running tool) cannot take a later
// call's result.
func (t *translator) modelContent(index int, c *genai.Content) error {
	clear(t.pendingCalls)
	msg := llms.Message{Role: llms.RoleAssistant}
	var text, thought strings.Builder
	var thoughtEnv *envelope
	for pi, p := range c.Parts {
		if isZeroPart(p) {
			continue
		}
		if f := hardPartField(p); f != "" {
			return unsupportedPart(index, "part "+f)
		}
		switch {
		case p.FunctionCall != nil:
			tc, err := t.functionCall(index, pi, p)
			if err != nil {
				return err
			}
			msg.ToolCalls = append(msg.ToolCalls, tc)
		case p.Thought:
			thought.WriteString(p.Text)
			env, res := partSignature(p)
			switch res {
			case sigNone:
			case sigInvalid:
				t.tr.droppedEnvelopes++
			case sigEnvelope, sigNative:
				if thoughtEnv == nil {
					thoughtEnv = &env
				}
			}
		case p.InlineData != nil || p.FileData != nil:
			return unsupportedPart(index, "model-role media part")
		case p.FunctionResponse != nil:
			return unsupportedPart(index, "function response in a model turn")
		default:
			text.WriteString(p.Text)
		}
	}
	msg.Content = text.String()
	switch {
	case thoughtEnv != nil:
		msg.Reasoning = thoughtEnv.reasoning(thought.String())
	case thought.Len() > 0:
		msg.Reasoning = &llms.ReasoningContent{Content: thought.String()}
	}
	if msg.Content == "" && len(msg.ToolCalls) == 0 && msg.Reasoning == nil {
		return nil
	}
	t.tr.messages = append(t.tr.messages, msg)
	return nil
}

// functionCall translates a FunctionCall part into a tool call.
func (t *translator) functionCall(index, partIndex int, p *genai.Part) (llms.ToolCall, error) {
	fc := p.FunctionCall
	if len(fc.PartialArgs) > 0 || (fc.WillContinue != nil && *fc.WillContinue) {
		return llms.ToolCall{}, unsupportedPart(index, "streamed function call arguments")
	}
	args := fc.Args
	if args == nil {
		args = map[string]any{}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return llms.ToolCall{}, fmt.Errorf("llmadk: contents[%d] function call %q arguments: %w", index, fc.Name, err)
	}
	id := fc.ID
	if id == "" {
		id = t.synthesizeID(index, partIndex, fc.Name)
		t.pendingCalls[fc.Name] = append(t.pendingCalls[fc.Name], id)
	}
	tc := llms.ToolCall{
		ID:       id,
		Type:     llms.ToolTypeFunction,
		Function: &llms.FunctionCall{Name: fc.Name, Arguments: string(raw)},
	}
	env, res := partSignature(p)
	switch res {
	case sigNone:
	case sigInvalid:
		t.tr.droppedEnvelopes++
	case sigEnvelope, sigNative:
		tc.Signature = env.Signature
		tc.SignatureProvider = env.Provider
	}
	return tc, nil
}

// synthesizeID gives an ID-less call from foreign history a deterministic ID of
// nine alphanumerics, valid for every provider, so re-sending the same history
// yields the same request and a prompt cache stays warm.
func (t *translator) synthesizeID(index, partIndex int, name string) string {
	seed := fmt.Sprintf("llmadk/%d/%d/%s", index, partIndex, name)
	id := openaicompat.NineCharToolCallID(seed)
	for t.tr.historyIDs[id] {
		seed += "+"
		id = openaicompat.NineCharToolCallID(seed)
	}
	t.tr.historyIDs[id] = true
	return id
}

// userContent translates a user turn: its function responses become tool
// messages, placed first because OpenAI-style APIs require tool results to
// follow the assistant's calls directly, then its text and media become one
// user message.
func (t *translator) userContent(index int, c *genai.Content) error {
	var parts []llms.ContentPart
	var texts []string
	for pi, p := range c.Parts {
		if isZeroPart(p) {
			continue
		}
		if f := hardPartField(p); f != "" {
			return unsupportedPart(index, "part "+f)
		}
		switch {
		case p.Thought:
			// ADK can carry another agent's thought into a user turn when it
			// rewrites foreign events; it is not this conversation's reasoning.
			continue
		case p.FunctionResponse != nil:
			msg, err := t.functionResponse(index, pi, p.FunctionResponse)
			if err != nil {
				return err
			}
			t.tr.messages = append(t.tr.messages, msg)
		case p.FunctionCall != nil:
			return unsupportedPart(index, "function call in a user turn")
		case p.InlineData != nil:
			part, err := inlinePart(index, p.InlineData)
			if err != nil {
				return err
			}
			if part.Type == llms.PartTypeImage {
				t.tr.hasImages = true
			}
			parts = append(parts, part)
			if part.Type == llms.PartTypeText {
				texts = append(texts, part.Text)
			}
		case p.FileData != nil:
			part, err := filePart(index, p.FileData)
			if err != nil {
				return err
			}
			t.tr.hasImages = true
			parts = append(parts, part)
		default:
			parts = append(parts, llms.NewTextPart(p.Text))
			texts = append(texts, p.Text)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	msg := llms.Message{Role: llms.RoleUser}
	if len(texts) == len(parts) {
		msg.Content = strings.Join(texts, "")
	} else {
		msg.Parts = parts
	}
	t.tr.messages = append(t.tr.messages, msg)
	return nil
}

// functionResponse translates a FunctionResponse part into a tool message.
func (t *translator) functionResponse(index, partIndex int, fr *genai.FunctionResponse) (llms.Message, error) {
	if len(fr.Parts) > 0 {
		return llms.Message{}, unsupportedPart(index, fmt.Sprintf("function response %q with media parts", fr.Name))
	}
	if fr.WillContinue != nil && *fr.WillContinue {
		return llms.Message{}, unsupportedPart(index, fmt.Sprintf("streamed function response %q", fr.Name))
	}
	id := fr.ID
	if id == "" {
		if queue := t.pendingCalls[fr.Name]; len(queue) > 0 {
			id, t.pendingCalls[fr.Name] = queue[0], queue[1:]
		} else {
			id = t.synthesizeID(index, partIndex, fr.Name)
		}
	}
	response := fr.Response
	if response == nil {
		response = map[string]any{}
	}
	body, err := json.Marshal(response)
	if err != nil {
		return llms.Message{}, fmt.Errorf("llmadk: contents[%d] function response %q: %w", index, fr.Name, err)
	}
	return llms.Message{Role: llms.RoleTool, ToolCallID: id, Name: fr.Name, Content: string(body)}, nil
}

// inlinePart translates inline data in a user turn. Images pass through;
// text-like data (an artifact ADK's load_artifacts tool attached) becomes text.
func inlinePart(index int, b *genai.Blob) (llms.ContentPart, error) {
	mime := strings.ToLower(strings.TrimSpace(b.MIMEType))
	switch {
	case strings.HasPrefix(mime, "image/"):
		return llms.NewImageBase64Part(base64.StdEncoding.EncodeToString(b.Data), mime), nil
	case strings.HasPrefix(mime, "text/") || mime == "application/json":
		text := string(b.Data)
		if b.DisplayName != "" {
			text = "[" + b.DisplayName + "]\n" + text
		}
		return llms.NewTextPart(text), nil
	default:
		return llms.ContentPart{}, unsupportedPart(index, fmt.Sprintf("inline data of type %q", b.MIMEType))
	}
}

// filePart translates a file reference in a user turn. Only images a provider
// can fetch over HTTP(S) are carried; gs:// and Gemini Files API URIs are only
// readable by Gemini.
func filePart(index int, f *genai.FileData) (llms.ContentPart, error) {
	uri := f.FileURI
	mime := strings.ToLower(f.MIMEType)
	if !strings.HasPrefix(mime, "image/") {
		return llms.ContentPart{}, unsupportedPart(index, fmt.Sprintf("file of type %q", f.MIMEType))
	}
	if !strings.HasPrefix(uri, "https://") && !strings.HasPrefix(uri, "http://") ||
		strings.Contains(uri, "generativelanguage.googleapis.com") {
		return llms.ContentPart{}, unsupportedPart(index, fmt.Sprintf("file URI %q, readable only by Gemini", uri))
	}
	return llms.NewImageURLPart(uri), nil
}

// pendingResult is the tool result sent for a call that has none yet: ADK
// records no response for a long-running or deferred tool until it finishes,
// and OpenAI- and Anthropic-style APIs reject a call without a result.
const pendingResult = `{"status":"pending","detail":"no result yet; do not call the tool again unless asked"}`

// repairToolResults makes every assistant tool call be followed by exactly one
// result. A call with no result gets pendingResult, telling the model the
// operation is outstanding. Duplicate results for one call (ADK re-sends a
// response after a tool confirmation) keep the last. Results are ordered as the
// calls are. A result that answers no call of the preceding assistant message
// is an orphan: every provider rejects a tool result without its call, so it is
// dropped and counted (reported as llmgo.orphans_dropped). ADK removes orphans
// that carry an ID itself; these are the ID-less ones it cannot match.
func repairToolResults(msgs []llms.Message) ([]llms.Message, int) {
	out := make([]llms.Message, 0, len(msgs))
	dropped := 0
	for i := 0; i < len(msgs); {
		msg := msgs[i]
		i++
		if msg.Role == llms.RoleTool {
			dropped++
			continue
		}
		out = append(out, msg)
		if msg.Role != llms.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		j := i
		for j < len(msgs) && msgs[j].Role == llms.RoleTool {
			j++
		}
		results := msgs[i:j]
		latest := map[string]int{}
		for k, r := range results {
			latest[r.ToolCallID] = k
		}
		answered := map[string]bool{}
		for _, tc := range msg.ToolCalls {
			if k, ok := latest[tc.ID]; ok {
				out = append(out, results[k])
			} else {
				name := ""
				if tc.Function != nil {
					name = tc.Function.Name
				}
				out = append(out, llms.Message{Role: llms.RoleTool, ToolCallID: tc.ID, Name: name, Content: pendingResult})
			}
			answered[tc.ID] = true
		}
		for _, r := range results {
			if !answered[r.ToolCallID] {
				dropped++
			}
		}
		i = j
	}
	return out, dropped
}
