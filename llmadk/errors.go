package llmadk

import (
	"errors"
	"fmt"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

var (
	// ErrRequestNil is returned for a nil *model.LLMRequest.
	ErrRequestNil = errors.New("llmadk: request is nil")
	// ErrNoContents is returned for a request with no contents.
	ErrNoContents = errors.New("llmadk: request has no contents")
	// ErrUnsupportedConfigField is returned for a GenerateContentConfig field
	// that has no equivalent in llm-go-sdk; the error names the field. Fields
	// that are not HARD can be ignored instead with WithIgnore.
	ErrUnsupportedConfigField = errors.New("llmadk: unsupported generation config field")
	// ErrUnsupportedPart is returned for a content part llm-go-sdk cannot carry,
	// such as audio or a gs:// file; the error names it.
	ErrUnsupportedPart = errors.New("llmadk: unsupported content part")
	// ErrUnsupportedTool is returned for a tool llm-go-sdk cannot offer, such as
	// Gemini's code execution; the error names it.
	ErrUnsupportedTool = errors.New("llmadk: unsupported tool")
	// ErrCapability is returned, with WithCapabilityChecks, for a request the
	// provider reports it cannot serve (tools, images or a JSON schema).
	ErrCapability = errors.New("llmadk: provider lacks a capability the request needs")
	// ErrFunctionCallArgs is returned when a model's tool-call arguments are not
	// a JSON object.
	ErrFunctionCallArgs = errors.New("llmadk: function call arguments are not a JSON object")
	// ErrIncompleteStream is returned when a provider's stream ends without a
	// final chunk. It wraps llms.ErrStreamInterrupted.
	ErrIncompleteStream = fmt.Errorf("llmadk: stream ended before its final chunk: %w", llms.ErrStreamInterrupted)
)
