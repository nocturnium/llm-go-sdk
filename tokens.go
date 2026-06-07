package llms

import (
	"unicode"
)

// TokenEstimator provides token count estimation for text.
// This is useful as a fallback when providers don't return token counts.
//
// The estimation uses heuristics based on common tokenizer behavior:
//   - Average of ~4 characters per token for English text
//   - Words and punctuation are counted separately
//   - Whitespace handling matches typical BPE tokenizers
//
// Note: These are estimates. Actual token counts vary by model and tokenizer.
type TokenEstimator struct {
	// CharsPerToken is the average characters per token (default: 4.0)
	CharsPerToken float64
}

// DefaultTokenEstimator returns a token estimator with default settings.
func DefaultTokenEstimator() *TokenEstimator {
	return &TokenEstimator{
		CharsPerToken: 4.0,
	}
}

// EstimateTokens estimates the number of tokens in a text string.
// Uses a hybrid approach combining character count and word count.
func (e *TokenEstimator) EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	charsPerToken := e.CharsPerToken
	if charsPerToken <= 0 {
		charsPerToken = 4.0
	}

	// Count characters (excluding whitespace for more accurate estimation)
	charCount := 0
	wordCount := 0
	inWord := false
	punctCount := 0

	for _, r := range text {
		if unicode.IsSpace(r) {
			if inWord {
				wordCount++
				inWord = false
			}
		} else if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			punctCount++
			if inWord {
				wordCount++
				inWord = false
			}
		} else {
			charCount++
			inWord = true
		}
	}

	// Count last word if text doesn't end with space
	if inWord {
		wordCount++
	}

	// Estimate using character-based approach
	charBasedEstimate := float64(charCount) / charsPerToken

	// Punctuation and symbols typically get their own tokens
	// Words also contribute (accounts for subword tokenization)
	// Use a weighted average for better accuracy
	estimate := charBasedEstimate + float64(punctCount)*0.8 + float64(wordCount)*0.1

	// Round up to be conservative (better to overestimate than underestimate)
	result := int(estimate + 0.5)
	if result < 1 && len(text) > 0 {
		result = 1
	}

	return result
}

// EstimateMessages estimates the total tokens for a slice of messages.
// Includes overhead for message formatting (role markers, separators).
func (e *TokenEstimator) EstimateMessages(messages []Message) int {
	if len(messages) == 0 {
		return 0
	}

	total := 0

	for _, msg := range messages {
		// Add overhead for message structure (role, separators)
		// Typical overhead is ~4 tokens per message for most formats
		total += 4

		// Estimate content
		if msg.Content != "" {
			total += e.EstimateTokens(msg.Content)
		}

		// Estimate parts content
		for _, part := range msg.Parts {
			if part.Type == "text" && part.Text != "" {
				total += e.EstimateTokens(part.Text)
			}
			// Images are typically handled differently (not text tokens)
		}

		// Estimate tool calls (function name + arguments)
		for _, tc := range msg.ToolCalls {
			total += 3 // overhead for tool call structure
			if tc.Function != nil {
				total += e.EstimateTokens(tc.Function.Name)
				total += e.EstimateTokens(tc.Function.Arguments)
			}
		}
	}

	// Add base overhead for the conversation format
	total += 3

	return total
}

// EstimateUsage creates a Usage struct with estimated token counts.
// Takes the input messages and the response content to estimate both
// prompt and completion tokens.
func (e *TokenEstimator) EstimateUsage(messages []Message, responseContent string) Usage {
	promptTokens := e.EstimateMessages(messages)
	completionTokens := e.EstimateTokens(responseContent)

	return Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}

// Global default estimator for convenience
var defaultEstimator = DefaultTokenEstimator()

// EstimateTokens estimates tokens in text using the default estimator.
func EstimateTokens(text string) int {
	return defaultEstimator.EstimateTokens(text)
}

// EstimateMessageTokens estimates tokens for messages using the default estimator.
func EstimateMessageTokens(messages []Message) int {
	return defaultEstimator.EstimateMessages(messages)
}

// EstimateUsageFromMessages creates an estimated Usage from messages and response.
func EstimateUsageFromMessages(messages []Message, responseContent string) Usage {
	return defaultEstimator.EstimateUsage(messages, responseContent)
}

// UsageOrEstimate returns the provided usage if it has token counts,
// otherwise returns an estimated usage based on the messages and response.
func UsageOrEstimate(usage Usage, messages []Message, responseContent string) Usage {
	// If we have any token counts, trust the provider's data
	if usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		// Fill in missing total if we have the components
		if usage.TotalTokens == 0 && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		return usage
	}

	// No token counts from provider, use estimation
	return EstimateUsageFromMessages(messages, responseContent)
}
