// Package tokenizer provides accurate, BPE-based token counting for use with the
// llms core package. It implements [llms.Tokenizer] on top of tiktoken (the
// encoding family used by OpenAI and OpenAI-compatible models).
//
// This package is opt-in: the core llms package depends on nothing here, so
// importing llms alone does not pull in a tokenizer. Wire a tokenizer into a
// [llms.TokenEstimator] for exact counts:
//
//	est := llms.DefaultTokenEstimator()
//	tk, err := tokenizer.ForModel("gpt-4o")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	est.Tokenizer = tk
//	n := est.EstimateTokens("hello world")
//
// Vocabularies are loaded from an embedded offline loader, so token counting
// never performs a network request at runtime.
package tokenizer

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

// offlineOnce installs the embedded (offline) BPE loader exactly once. tiktoken's
// loader is process-global, so we set it lazily on first use rather than in an
// init() to avoid clobbering a loader a caller may have set deliberately.
var offlineOnce sync.Once

func useOfflineLoader() {
	offlineOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	})
}

// tokenizer adapts a tiktoken encoder to the [llms.Tokenizer] interface.
type tokenizer struct {
	enc *tiktoken.Tiktoken
}

// CountTokens returns the number of tokens text encodes to, counting ordinary
// text only (special tokens are treated as ordinary text, never as control
// tokens) so untrusted input cannot inject special-token boundaries.
func (t *tokenizer) CountTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(t.enc.EncodeOrdinary(text))
}

// ForModel returns a [llms.Tokenizer] for the given model name (for example
// "gpt-4o", "gpt-4", or "gpt-3.5-turbo"). It returns an error if the model's
// encoding is unknown; use [ForEncoding] directly when you know the encoding.
func ForModel(model string) (llms.Tokenizer, error) {
	useOfflineLoader()
	enc, err := tiktoken.EncodingForModel(model)
	if err != nil {
		return nil, err
	}
	return &tokenizer{enc: enc}, nil
}

// ForEncoding returns a [llms.Tokenizer] for a named tiktoken encoding, for
// example "o200k_base" (GPT-4o family) or "cl100k_base" (GPT-4 / GPT-3.5).
func ForEncoding(encoding string) (llms.Tokenizer, error) {
	useOfflineLoader()
	enc, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, err
	}
	return &tokenizer{enc: enc}, nil
}
