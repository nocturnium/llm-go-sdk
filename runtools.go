package llms

import (
	"context"
	"errors"
	"fmt"
)

const (
	defaultRunToolsMaxIterations = 10
	defaultRunToolsConcurrency   = 8
)

// ErrMaxIterations is returned when RunTools reaches the configured iteration guard.
var ErrMaxIterations = errors.New("llms: maximum tool iterations exceeded")

// RunToolsOption configures RunTools.
type RunToolsOption func(*runToolsConfig)

type runToolsConfig struct {
	maxIterations int
	concurrency   int
	onStep        func(iteration int, resp *Response)
	callOptions   []CallOption
}

func defaultRunToolsConfig() runToolsConfig {
	return runToolsConfig{
		maxIterations: defaultRunToolsMaxIterations,
		concurrency:   defaultRunToolsConcurrency,
	}
}

func applyRunToolsOptions(opts ...RunToolsOption) runToolsConfig {
	cfg := defaultRunToolsConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.maxIterations <= 0 {
		cfg.maxIterations = defaultRunToolsMaxIterations
	}
	if cfg.concurrency <= 0 {
		cfg.concurrency = defaultRunToolsConcurrency
	}
	return cfg
}

// WithMaxIterations sets the maximum model-tool loop iterations RunTools may execute.
// Values less than or equal to zero use the default of 10.
func WithMaxIterations(n int) RunToolsOption {
	return func(cfg *runToolsConfig) {
		cfg.maxIterations = n
	}
}

// WithOnStep sets an observability callback invoked once after each model response.
// The iteration argument is 1-based. RunTools does not mutate the response before
// invoking the callback.
func WithOnStep(fn func(iteration int, resp *Response)) RunToolsOption {
	return func(cfg *runToolsConfig) {
		cfg.onStep = fn
	}
}

// WithToolConcurrency bounds the number of tool calls RunTools executes concurrently
// within a single model turn. Values less than or equal to zero use the default of 8.
func WithToolConcurrency(n int) RunToolsOption {
	return func(cfg *runToolsConfig) {
		cfg.concurrency = n
	}
}

// WithCallOptions sets CallOption values to apply to every model turn.
// RunTools prepends these options before WithTools(registry.Tools()), so caller
// options can set model, temperature, max tokens, reasoning, response format,
// and other per-call fields, while the registry tools always take precedence
// for the tools field.
func WithCallOptions(options ...CallOption) RunToolsOption {
	return func(cfg *runToolsConfig) {
		cfg.callOptions = append([]CallOption(nil), options...)
	}
}

// RunTools runs a full tool-calling agent loop using llm and registry.
//
// RunTools copies the incoming messages into a fresh transcript, then repeatedly
// calls GenerateContent with any WithCallOptions options followed by
// WithTools(registry.Tools()). Because WithTools is applied last, registry tools
// take precedence for the tools field while other forwarded call options such as
// model, temperature, max tokens, reasoning, and response format are honored. If
// the model returns no tool calls, RunTools appends the final assistant message
// and returns the response and full transcript. If the model returns tool calls,
// RunTools appends the assistant message carrying those calls, executes the calls
// concurrently with a bounded per-turn concurrency limit, appends one RoleTool
// message per call, and calls the model again.
//
// Tool-result ordering is deterministic: messages are appended in the same order as
// resp.ToolCalls regardless of completion order. Context cancellation is checked
// before model calls, while scheduling tool calls, and while waiting for tool
// results; if ctx is canceled, RunTools returns promptly with ctx.Err(), the last
// response, and the transcript built so far. Tool handlers receive a context that
// is canceled when RunTools returns early due to an error or context cancellation,
// and handlers should honor that context to abort in-flight work. ToolRegistry.Handle
// converts handler errors into tool-result messages so the model can react. If
// dispatch itself fails such as a missing tool or malformed function call, RunTools
// uses the same model-reacts policy by appending a tool-result message containing
// the error text.
//
// If the loop reaches the configured iteration guard before the model stops asking
// for tools, RunTools returns the last response, the transcript, and an error that
// wraps ErrMaxIterations.
func RunTools(ctx context.Context, llm LLM, messages []Message, registry *ToolRegistry, opts ...RunToolsOption) (*Response, []Message, error) {
	cfg := applyRunToolsOptions(opts...)
	transcript := append([]Message(nil), messages...)
	var lastResp *Response

	for iteration := 1; iteration <= cfg.maxIterations; iteration++ {
		select {
		case <-ctx.Done():
			return lastResp, transcript, ctx.Err()
		default:
		}

		callOptions := make([]CallOption, 0, len(cfg.callOptions)+1)
		callOptions = append(callOptions, cfg.callOptions...)
		callOptions = append(callOptions, WithTools(registry.Tools()))
		resp, err := llm.GenerateContent(ctx, transcript, callOptions...)
		if err != nil {
			return nil, transcript, err
		}
		lastResp = resp

		if cfg.onStep != nil {
			cfg.onStep(iteration, resp)
		}

		assistantMessage := Message{
			Role:      RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
			// Preserve reasoning so providers that require the thinking block to be
			// replayed on the next turn (Anthropic extended thinking, OpenAI
			// Responses) can re-emit it instead of 400-ing on turn 2+.
			Reasoning: resp.Reasoning,
		}
		transcript = append(transcript, assistantMessage)

		if len(resp.ToolCalls) == 0 {
			return resp, transcript, nil
		}

		toolMessages, err := runToolCalls(ctx, registry, resp.ToolCalls, cfg.concurrency)
		if err != nil {
			return lastResp, transcript, err
		}
		transcript = append(transcript, toolMessages...)
	}

	return lastResp, transcript, fmt.Errorf("run tools: %w", ErrMaxIterations)
}

type runToolResult struct {
	index   int
	message Message
}

func runToolCalls(ctx context.Context, registry *ToolRegistry, calls []ToolCall, concurrency int) ([]Message, error) {
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if concurrency > len(calls) {
		concurrency = len(calls)
	}
	sem := make(chan struct{}, concurrency)
	results := make(chan runToolResult, len(calls))

	for i, tc := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case sem <- struct{}{}:
		}
		if err := ctx.Err(); err != nil {
			<-sem
			return nil, err
		}

		go func(index int, call ToolCall) {
			defer func() { <-sem }()
			// A panicking tool handler must not crash the host; turn it into a
			// tool-error message the model can react to.
			defer func() {
				if r := recover(); r != nil {
					results <- runToolResult{
						index: index,
						message: ToolResultError(call.ID, toolCallName(call),
							fmt.Errorf("tool %q panicked: %v", toolCallName(call), r)),
					}
				}
			}()

			if err := callCtx.Err(); err != nil {
				return
			}
			msg, err := registry.Handle(callCtx, call)
			if err != nil {
				msg = ToolResultError(call.ID, toolCallName(call), err)
			}

			results <- runToolResult{
				index:   index,
				message: msg,
			}
		}(i, tc)
	}

	messages := make([]Message, len(calls))
	for range calls {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-results:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			messages[result.index] = result.message
		}
	}

	return messages, nil
}

func toolCallName(tc ToolCall) string {
	if tc.Function == nil {
		return ""
	}
	return tc.Function.Name
}
