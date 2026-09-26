package llmadk

import (
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Option configures a Model built by NewModel.
type Option func(*options)

type options struct {
	name             string
	callOptions      []llms.CallOption
	ignore           map[Field]bool
	webSearch        bool
	capabilityChecks bool
	timeout          time.Duration
}

func defaultOptions() *options {
	return &options{ignore: map[Field]bool{}}
}

func apply(opts ...Option) *options {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithName sets the name the Model reports to ADK. It defaults to the wrapped
// LLM's model. ADK treats a name starting with "gemini-" as a Gemini model: it
// then sends output schemas as a set_model_response tool of its own instead of
// as a response schema, so choose a Gemini name only for a Gemini model.
func WithName(name string) Option {
	return func(o *options) { o.name = name }
}

// WithCallOptions adds call options to every request, applied before the
// options derived from the ADK request, so a request's own settings win.
func WithCallOptions(opts ...llms.CallOption) Option {
	return func(o *options) { o.callOptions = append(o.callOptions, opts...) }
}

// WithIgnore drops the named fields from requests instead of rejecting them,
// for configs written for Gemini (safety settings, top-k, a seed). Fields whose
// loss would change the shape of the result, such as a candidate count above
// one, are rejected regardless.
func WithIgnore(fields ...Field) Option {
	return func(o *options) {
		for _, f := range fields {
			o.ignore[f] = true
		}
	}
}

// WithWebSearch declares that the wrapped provider runs a web search when asked
// (llms.WithWebSearch), so ADK's GoogleSearch tool maps onto it and its
// results onto grounding metadata. Only Z.AI does today; without this option
// the GoogleSearch tool is rejected rather than silently ignored.
func WithWebSearch() Option {
	return func(o *options) { o.webSearch = true }
}

// WithCapabilityChecks rejects, with ErrCapability, a request that asks for
// tools, images or a JSON schema from a provider that reports not supporting
// them. It is off by default because several providers report "false" for
// features that depend on the model (Ollama reports no tools for every model).
func WithCapabilityChecks() Option {
	return func(o *options) { o.capabilityChecks = true }
}

// WithTimeout bounds each model call. A request's HTTPOptions.Timeout, when
// set, takes precedence.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}
