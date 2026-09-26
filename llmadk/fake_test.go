package llmadk

import (
	"context"
	"strings"
	"sync"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// fakeLLM is a scripted llms.LLM. Each call consumes the next scripted response
// and records the messages and options it was called with. Stream delivers the
// response as one content chunk per word, one reasoning chunk, and a final chunk
// carrying the tool calls and metadata, the shape every provider here follows.
type fakeLLM struct {
	mu        sync.Mutex
	provider  llms.Provider
	model     string
	responses []*llms.Response
	calls     []fakeCall
	// streamDone is closed when a Stream goroutine exits.
	streamDone chan struct{}
	// noTerminal makes Stream close without a final chunk.
	noTerminal bool
	caps       *llms.Capabilities
}

type fakeCall struct {
	messages []llms.Message
	opts     *llms.CallOptions
}

func newFake(responses ...*llms.Response) *fakeLLM {
	return &fakeLLM{provider: llms.ProviderAnthropic, model: "fake-model", responses: responses}
}

func (f *fakeLLM) next(messages []llms.Message, options []llms.CallOption) *llms.Response {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{messages: messages, opts: llms.ApplyOptions(options...)})
	if len(f.responses) == 0 {
		return &llms.Response{Content: "done", FinishReason: llms.FinishReasonStop}
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	cp := *r
	cp.ToolCalls = append([]llms.ToolCall(nil), r.ToolCalls...)
	return &cp
}

func (f *fakeLLM) lastCall() fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func (f *fakeLLM) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeLLM) GenerateContent(_ context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	resp := f.next(messages, options)
	llms.StampResponse(resp, f.provider, f.model)
	return resp, nil
}

func (f *fakeLLM) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	resp := f.next(messages, options)
	out := make(chan llms.StreamChunk)
	done := f.streamDone
	go func() {
		defer func() {
			close(out)
			if done != nil {
				close(done)
			}
		}()
		s := llms.NewStreamSender(ctx, out, 0)
		s.SetIdentity(f.provider, f.model)
		if resp.Reasoning != nil {
			if s.ForwardTerminalOnEarlyExit(s.Send(llms.StreamChunk{Reasoning: resp.Reasoning})) {
				return
			}
		}
		for _, w := range strings.SplitAfter(resp.Content, " ") {
			if w == "" {
				continue
			}
			if s.ForwardTerminalOnEarlyExit(s.Send(llms.StreamChunk{Content: w})) {
				return
			}
		}
		if f.noTerminal {
			return
		}
		usage := resp.Usage
		s.SendFinal(llms.StreamChunk{
			ToolCalls:    resp.ToolCalls,
			FinishReason: resp.FinishReason,
			Usage:        &usage,
			Adjustments:  resp.Adjustments,
		})
	}()
	return out, nil
}

func (f *fakeLLM) Provider() llms.Provider { return f.provider }
func (f *fakeLLM) Model() string           { return f.model }

func (f *fakeLLM) Capabilities() llms.Capabilities {
	if f.caps != nil {
		return *f.caps
	}
	return llms.Capabilities{Streaming: true, Tools: true, Vision: true, JSONMode: true}
}

func toolCall(id, name, args string) llms.ToolCall {
	return llms.ToolCall{ID: id, Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: name, Arguments: args}}
}
