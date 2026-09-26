package llms

import (
	"context"
	"reflect"
	"regexp"
	"testing"
)

func TestStampResponse(t *testing.T) {
	t.Run("fills empty identity and stamps reasoning and signatures", func(t *testing.T) {
		resp := &Response{
			Reasoning: &ReasoningContent{Content: "think", Signature: "sig"},
			ToolCalls: []ToolCall{
				{ID: "a", Signature: "call-sig"},
				{ID: "b"},
			},
		}
		StampResponse(resp, ProviderAnthropic, "claude-x")
		if resp.Provider != ProviderAnthropic || resp.Model != "claude-x" {
			t.Errorf("identity = %q/%q, want anthropic/claude-x", resp.Provider, resp.Model)
		}
		if resp.Reasoning.Provider != ProviderAnthropic || resp.Reasoning.Model != "claude-x" {
			t.Errorf("reasoning stamp = %q/%q", resp.Reasoning.Provider, resp.Reasoning.Model)
		}
		if resp.ToolCalls[0].SignatureProvider != ProviderAnthropic {
			t.Errorf("signed call stamp = %q", resp.ToolCalls[0].SignatureProvider)
		}
		if resp.ToolCalls[1].SignatureProvider != "" {
			t.Errorf("unsigned call stamped %q, want empty", resp.ToolCalls[1].SignatureProvider)
		}
	})

	t.Run("keeps values already set further down the chain", func(t *testing.T) {
		resp := &Response{
			Provider:  ProviderGemini,
			Model:     "gemini-y",
			Reasoning: &ReasoningContent{Provider: ProviderGemini, Model: "gemini-y"},
			ToolCalls: []ToolCall{{Signature: "s", SignatureProvider: ProviderGemini}},
		}
		StampResponse(resp, ProviderOpenAI, "gpt")
		if resp.Provider != ProviderGemini || resp.Model != "gemini-y" ||
			resp.Reasoning.Provider != ProviderGemini || resp.ToolCalls[0].SignatureProvider != ProviderGemini {
			t.Errorf("stamps overwritten: %+v", resp)
		}
	})

	t.Run("nil response is a no-op", func(t *testing.T) {
		StampResponse(nil, ProviderOpenAI, "gpt")
	})
}

func TestStreamSender_SetIdentity(t *testing.T) {
	shared := &ReasoningContent{Content: "think", Signature: "sig"}
	sharedCalls := []ToolCall{{ID: "a", Signature: "call-sig"}}

	ch := make(chan StreamChunk, 4)
	s := NewStreamSender(context.Background(), ch, 0)
	s.SetIdentity(ProviderGemini, "gemini-y")
	s.Send(StreamChunk{Reasoning: shared})
	s.Send(StreamChunk{Content: "text"})
	s.SendFinal(StreamChunk{ToolCalls: sharedCalls})
	close(ch)

	var got []StreamChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 3 {
		t.Fatalf("got %d chunks, want 3", len(got))
	}
	if got[0].Reasoning.Provider != ProviderGemini || got[0].Reasoning.Model != "gemini-y" {
		t.Errorf("reasoning chunk stamp = %q/%q", got[0].Reasoning.Provider, got[0].Reasoning.Model)
	}
	if got[1].Provider != "" {
		t.Errorf("non-final chunk carries identity %q", got[1].Provider)
	}
	final := got[2]
	if !final.Done || final.Provider != ProviderGemini || final.Model != "gemini-y" {
		t.Errorf("final chunk identity = done %v %q/%q", final.Done, final.Provider, final.Model)
	}
	if final.ToolCalls[0].SignatureProvider != ProviderGemini {
		t.Errorf("final tool call stamp = %q", final.ToolCalls[0].SignatureProvider)
	}
	// The producer's own values must not be written through the chunk.
	if shared.Provider != "" || sharedCalls[0].SignatureProvider != "" {
		t.Errorf("stamping mutated the producer's state: %+v %+v", shared, sharedCalls[0])
	}
}

func TestStreamSender_NoIdentityPassesStampsThrough(t *testing.T) {
	ch := make(chan StreamChunk, 1)
	s := NewStreamSender(context.Background(), ch, 0)
	s.SendFinal(StreamChunk{Provider: ProviderAnthropic, Model: "m"})
	close(ch)
	got := <-ch
	if got.Provider != ProviderAnthropic || got.Model != "m" {
		t.Errorf("forwarded identity = %q/%q", got.Provider, got.Model)
	}
}

func TestStreamSender_ErrorTerminalCarriesNoIdentity(t *testing.T) {
	ch := make(chan StreamChunk, 1)
	s := NewStreamSender(context.Background(), ch, 0)
	s.SetIdentity(ProviderOpenAI, "gpt")
	s.DeliverTerminal(StreamChunk{Error: context.Canceled})
	close(ch)
	if got := <-ch; got.Provider != "" {
		t.Errorf("error chunk identity = %q, want empty", got.Provider)
	}
}

var toolCallIDPattern = regexp.MustCompile(`^[a-zA-Z0-9]{9}$`)

func TestNewToolCallID(t *testing.T) {
	seen := map[string]bool{}
	for range 10000 {
		id := NewToolCallID()
		if !toolCallIDPattern.MatchString(id) {
			t.Fatalf("NewToolCallID() = %q, want 9 chars of [a-zA-Z0-9]", id)
		}
		if seen[id] {
			t.Fatalf("NewToolCallID() repeated %q within 10000 draws", id)
		}
		seen[id] = true
	}
}

func TestEnsureToolCallIDs(t *testing.T) {
	calls := []ToolCall{{ID: "keep"}, {ID: ""}, {ID: "keep"}, {ID: "other"}}
	EnsureToolCallIDs(calls)
	if calls[0].ID != "keep" || calls[3].ID != "other" {
		t.Errorf("unique IDs changed: %q %q", calls[0].ID, calls[3].ID)
	}
	if !toolCallIDPattern.MatchString(calls[1].ID) {
		t.Errorf("empty ID replaced with %q", calls[1].ID)
	}
	if calls[2].ID == "keep" || !toolCallIDPattern.MatchString(calls[2].ID) {
		t.Errorf("duplicate ID replaced with %q", calls[2].ID)
	}
	EnsureToolCallIDs(nil)
}

func TestDropForeignReplay(t *testing.T) {
	own := &ReasoningContent{Content: "a", Signature: "s", Provider: ProviderAnthropic}
	foreign := &ReasoningContent{Content: "b", Signature: "t", Provider: ProviderGemini}
	unstamped := &ReasoningContent{Content: "c", Signature: "u"}
	messages := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Reasoning: own},
		{Role: RoleAssistant, Reasoning: foreign, ToolCalls: []ToolCall{
			{ID: "1", Signature: "g", SignatureProvider: ProviderGemini},
			{ID: "2", Signature: "a", SignatureProvider: ProviderAnthropic},
			{ID: "3", Signature: "legacy"},
		}},
		{Role: RoleAssistant, Reasoning: unstamped},
	}
	original := make([]Message, len(messages))
	copy(original, messages)
	originalCalls := append([]ToolCall(nil), messages[2].ToolCalls...)

	out := DropForeignReplay(messages, ProviderAnthropic)

	if out[1].Reasoning != own || out[3].Reasoning != unstamped {
		t.Error("own or unstamped reasoning was dropped")
	}
	if out[2].Reasoning != nil {
		t.Error("foreign reasoning kept")
	}
	calls := out[2].ToolCalls
	if calls[0].Signature != "" || calls[0].SignatureProvider != "" {
		t.Errorf("foreign signature kept: %+v", calls[0])
	}
	if calls[1].Signature != "a" || calls[2].Signature != "legacy" {
		t.Errorf("own or unstamped signature dropped: %+v %+v", calls[1], calls[2])
	}
	if !reflect.DeepEqual(messages, original) || !reflect.DeepEqual(messages[2].ToolCalls, originalCalls) {
		t.Error("input messages were modified")
	}

	t.Run("returns the input when nothing is foreign", func(t *testing.T) {
		in := []Message{{Role: RoleAssistant, Reasoning: own}}
		if got := DropForeignReplay(in, ProviderAnthropic); &got[0] != &in[0] {
			t.Error("a copy was made with nothing to drop")
		}
	})
}

func TestReasoningContent_ReplayableBy(t *testing.T) {
	var nilRC *ReasoningContent
	if nilRC.ReplayableBy(ProviderOpenAI) {
		t.Error("nil reasoning replayable")
	}
	if !(&ReasoningContent{}).ReplayableBy(ProviderOpenAI) {
		t.Error("unstamped reasoning not replayable")
	}
	if (&ReasoningContent{Provider: ProviderGemini}).ReplayableBy(ProviderOpenAI) {
		t.Error("foreign reasoning replayable")
	}
}

// filledValue returns a value of type T with every exported field set to a
// non-zero value, so a round-trip that drops a field shows up as a diff. New
// fields are covered without editing the test.
func filledValue[T any](t *testing.T) T {
	t.Helper()
	var v T
	rv := reflect.ValueOf(&v).Elem()
	for i := range rv.NumField() {
		f := rv.Field(i)
		if !rv.Type().Field(i).IsExported() {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString("x-" + rv.Type().Field(i).Name)
		case reflect.Int:
			f.SetInt(int64(i + 1))
		case reflect.Map:
			f.Set(reflect.ValueOf(map[string]any{"k": "v"}))
		case reflect.Pointer:
			if f.Type() == reflect.TypeOf(&FunctionCall{}) {
				f.Set(reflect.ValueOf(&FunctionCall{Name: "fn", Arguments: "{}"}))
				continue
			}
			t.Fatalf("filledValue: unhandled pointer field %s", rv.Type().Field(i).Name)
		default:
			t.Fatalf("filledValue: unhandled kind %s for field %s", f.Kind(), rv.Type().Field(i).Name)
		}
	}
	return v
}

// TestProvenanceSurvivesCopies guards the stamps (and every other field) of
// ReasoningContent and ToolCall through the root paths that copy them. A path
// that rebuilds either struct field by field silently drops a new field, and a
// dropped stamp fails open: the reasoning replays to a provider that rejects it.
func TestProvenanceSurvivesCopies(t *testing.T) {
	rc := filledValue[ReasoningContent](t)
	tc := filledValue[ToolCall](t)

	t.Run("Clone", func(t *testing.T) {
		if got := rc.Clone(); !reflect.DeepEqual(*got, rc) {
			t.Errorf("Clone lost fields:\n got %+v\nwant %+v", *got, rc)
		}
	})

	t.Run("cloneResponse", func(t *testing.T) {
		resp := &Response{Reasoning: &rc, ToolCalls: []ToolCall{tc}, Adjustments: []string{"a"}}
		got := cloneResponse(resp)
		if !reflect.DeepEqual(got, resp) {
			t.Errorf("cloneResponse lost fields:\n got %+v\nwant %+v", got, resp)
		}
	})

	t.Run("CollectStream", func(t *testing.T) {
		stream := streamFromChunks(
			StreamChunk{Reasoning: &rc},
			StreamChunk{
				Done:         true,
				ToolCalls:    []ToolCall{tc},
				Provider:     ProviderOpenAI,
				Model:        "m",
				ModelVersion: "m-1",
				Adjustments:  []string{"a"},
			},
		)
		res, err := CollectStream(stream)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*res.Reasoning, rc) {
			t.Errorf("CollectStream reasoning:\n got %+v\nwant %+v", *res.Reasoning, rc)
		}
		if !reflect.DeepEqual(res.ToolCalls, []ToolCall{tc}) {
			t.Errorf("CollectStream tool calls:\n got %+v\nwant %+v", res.ToolCalls, tc)
		}
		if res.Provider != ProviderOpenAI || res.Model != "m" || res.ModelVersion != "m-1" ||
			!reflect.DeepEqual(res.Adjustments, []string{"a"}) {
			t.Errorf("CollectStream identity: %+v", res)
		}
	})
}

func TestStreamSender_SetAdjustments(t *testing.T) {
	ch := make(chan StreamChunk, 2)
	s := NewStreamSender(context.Background(), ch, 0)
	s.SetAdjustments([]string{"p.changed"})
	s.Send(StreamChunk{Content: "x"})
	s.SendFinal(StreamChunk{})
	close(ch)
	if first := <-ch; first.Adjustments != nil {
		t.Errorf("non-final chunk carries adjustments %v", first.Adjustments)
	}
	if final := <-ch; len(final.Adjustments) != 1 || final.Adjustments[0] != "p.changed" {
		t.Errorf("final adjustments = %v", final.Adjustments)
	}
}
