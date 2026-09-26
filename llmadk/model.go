package llmadk

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/middleware/resilience"
)

// Model is an llms.LLM exposed to ADK as a model.LLM.
type Model struct {
	llm  llms.LLM
	name string
	opts *options
}

var _ model.LLM = (*Model)(nil)

// NewModel adapts llm to ADK's model.LLM. llm may be any provider client or a
// middleware chain around one. It returns an error for a nil llm.
func NewModel(llm llms.LLM, opts ...Option) (*Model, error) {
	if llm == nil {
		return nil, errors.New("llmadk: NewModel needs an llms.LLM")
	}
	o := apply(opts...)
	name := o.name
	if name == "" {
		name = llm.Model()
	}
	return &Model{llm: llm, name: name, opts: o}, nil
}

// Name returns the model name ADK sees (see WithName).
func (m *Model) Name() string { return m.name }

// LLM returns the wrapped llms.LLM.
func (m *Model) LLM() llms.LLM { return m.llm }

// GenerateContent runs one model turn. With stream false it yields exactly one
// complete response. With stream true it yields a partial response per text or
// thought delta and then one complete response carrying the whole turn, its
// tool calls, usage and finish reason, which is the one ADK acts on. Every
// range over the returned sequence makes a new call.
func (m *Model) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		tr, err := m.translate(req)
		if err != nil {
			yield(nil, err)
			return
		}
		if err := m.checkCapabilities(tr); err != nil {
			yield(nil, err)
			return
		}
		if tr.droppedEnvelopes > 0 {
			slog.WarnContext(ctx, "llmadk: dropped undecodable reasoning envelopes from session history",
				"count", tr.droppedEnvelopes, "model", m.name)
		}
		// Shadowed, not reassigned: the sequence may be ranged more than once,
		// and each range must start from the caller's context rather than the
		// deadline an earlier range set.
		ctx := ctx
		if timeout := m.timeout(tr); timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		callOpts := append(append([]llms.CallOption(nil), m.opts.callOptions...), tr.callOptions...)
		rt := m.responseTranslator(tr, callOpts)
		if stream {
			m.stream(ctx, tr, rt, callOpts, yield)
			return
		}
		resp, err := m.llm.GenerateContent(ctx, tr.messages, callOpts...)
		if err != nil {
			yield(nil, wrap(err))
			return
		}
		out, err := rt.convert(resp)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(out, nil)
	}
}

func (m *Model) timeout(tr *translation) time.Duration {
	if tr.timeout > 0 {
		return tr.timeout
	}
	return m.opts.timeout
}

// responseTranslator prepares response conversion. Reasoning the provider did
// not stamp is stamped with the wrapped client's provider and the requested
// model, except behind a fallback chain, whose serving entry is unknown here.
func (m *Model) responseTranslator(tr *translation, callOpts []llms.CallOption) *responseTranslator {
	rt := &responseTranslator{tr: tr}
	if _, isRouter := llms.UnwrapAll(m.llm).(*resilience.FallbackChain); isRouter {
		return rt
	}
	rt.fallbackProvider = m.llm.Provider()
	rt.fallbackModel = llms.ApplyOptions(callOpts...).Model
	if rt.fallbackModel == "" {
		rt.fallbackModel = m.llm.Model()
	}
	return rt
}

// wrap prefixes a provider error while keeping it matchable with errors.Is
// against the llms sentinels (ErrRateLimited and the rest).
func wrap(err error) error {
	return fmt.Errorf("llmadk: %w", err)
}

// checkCapabilities applies WithCapabilityChecks.
func (m *Model) checkCapabilities(tr *translation) error {
	if !m.opts.capabilityChecks {
		return nil
	}
	caps := llms.GetCapabilities(m.llm)
	switch {
	case tr.hasTools && !caps.Tools:
		return fmt.Errorf("%w: tools (%s)", ErrCapability, m.llm.Provider())
	case tr.hasImages && !caps.Vision:
		return fmt.Errorf("%w: images (%s)", ErrCapability, m.llm.Provider())
	case tr.hasSchema && !caps.JSONMode:
		return fmt.Errorf("%w: JSON schema (%s)", ErrCapability, m.llm.Provider())
	}
	return nil
}

// stream runs a streaming turn, yielding partial responses and then the
// complete one. When the turn ends early (the consumer stops, the context is
// canceled, or the stream fails) the rest of the provider's channel is drained
// in the background; a provider that never closes its channel leaves that
// goroutine waiting, which the SDK's own providers never do.
func (m *Model) stream(ctx context.Context, tr *translation, rt *responseTranslator, callOpts []llms.CallOption,
	yield func(*model.LLMResponse, error) bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	chunks, err := m.llm.Stream(ctx, tr.messages, callOpts...)
	if err != nil {
		yield(nil, wrap(err))
		return
	}
	// stopped drains the rest of the stream in the background after the
	// consumer stops or the stream fails, so the provider goroutine can finish
	// its sends and exit without holding the caller up.
	stopped := func() {
		cancel()
		llms.DrainStream(chunks)
	}

	var acc accumulator
	for {
		var chunk llms.StreamChunk
		var open bool
		select {
		case <-ctx.Done():
			// A provider that does not close its stream on cancellation must not
			// hold the consumer; the rest is drained in the background.
			stopped()
			yield(nil, ctx.Err())
			return
		case chunk, open = <-chunks:
		}
		if !open {
			break
		}
		if chunk.Error != nil {
			stopped()
			yield(nil, wrap(chunk.Error))
			return
		}
		acc.add(chunk)
		if chunk.Done {
			stopped()
			out, err := rt.convert(acc.response())
			if err != nil {
				yield(nil, err)
				return
			}
			yield(out, nil)
			return
		}
		if partial := rt.partial(chunk); partial != nil {
			if !yield(partial, nil) {
				stopped()
				return
			}
		}
	}
	if err := ctx.Err(); err != nil {
		yield(nil, err)
		return
	}
	yield(nil, ErrIncompleteStream)
}

// partial renders a delta chunk as a partial response, or nil when it carries
// nothing to show.
func (rt *responseTranslator) partial(chunk llms.StreamChunk) *model.LLMResponse {
	var parts []*genai.Part
	if chunk.Reasoning != nil && chunk.Reasoning.Content != "" && !rt.tr.hideThoughts {
		parts = append(parts, &genai.Part{Text: chunk.Reasoning.Content, Thought: true})
	}
	if chunk.Content != "" && !rt.tr.structured {
		parts = append(parts, &genai.Part{Text: chunk.Content})
	}
	if len(parts) == 0 {
		return nil
	}
	return &model.LLMResponse{
		Content: &genai.Content{Role: string(genai.RoleModel), Parts: parts},
		Partial: true,
	}
}

// accumulator rebuilds the complete response from a stream, the same way
// llms.CollectStream does, keeping the final chunk's identity and the
// reasoning's signature, metadata and provenance stamp.
type accumulator struct {
	resp      llms.Response
	reasoning *llms.ReasoningContent
}

func (a *accumulator) add(chunk llms.StreamChunk) {
	a.resp.Content += chunk.Content
	if rc := chunk.Reasoning; rc != nil {
		if a.reasoning == nil {
			a.reasoning = &llms.ReasoningContent{}
		}
		a.reasoning.Content += rc.Content
		if rc.Signature != "" {
			a.reasoning.Signature = rc.Signature
		}
		if rc.Metadata != nil {
			a.reasoning.Metadata = rc.Metadata
		}
		if rc.Tokens != 0 {
			a.reasoning.Tokens = rc.Tokens
		}
		if rc.Provider != "" {
			a.reasoning.Provider, a.reasoning.Model = rc.Provider, rc.Model
		}
	}
	if chunk.Done {
		a.resp.ToolCalls = chunk.ToolCalls
		a.resp.FinishReason = chunk.FinishReason
		if chunk.Usage != nil {
			a.resp.Usage = *chunk.Usage
		}
		a.resp.ServiceTier = chunk.ServiceTier
		a.resp.Provider = chunk.Provider
		a.resp.Model = chunk.Model
		a.resp.ModelVersion = chunk.ModelVersion
		a.resp.Adjustments = chunk.Adjustments
	}
}

func (a *accumulator) response() *llms.Response {
	a.resp.Reasoning = a.reasoning
	return &a.resp
}
