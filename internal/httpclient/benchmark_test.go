package httpclient

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func BenchmarkSSEReader_SimpleEvents(b *testing.B) {
	data := strings.Repeat("data: hello world\n\n", 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
		for {
			_, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
		}
		_ = reader.Close()
	}
}

func BenchmarkSSEReader_JSONEvents(b *testing.B) {
	data := strings.Repeat(`data: {"id":"123","content":"hello world","done":false}
`, 100) + "data: [DONE]\n\n"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
		for {
			_, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
		}
		_ = reader.Close()
	}
}

func BenchmarkSSEReader_WithEventTypes(b *testing.B) {
	data := strings.Repeat(`event: message_delta
data: {"type":"content_block_delta","delta":{"text":"hello"}}

`, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
		for {
			_, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
		}
		_ = reader.Close()
	}
}

func BenchmarkSSEReader_MultilineData(b *testing.B) {
	data := strings.Repeat("data: line1\ndata: line2\ndata: line3\n\n", 50)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
		for {
			_, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
		}
		_ = reader.Close()
	}
}

func BenchmarkSSEReader_LargePayload(b *testing.B) {
	// Simulate large JSON payloads
	largeContent := strings.Repeat("x", 10000)
	data := "data: {\"content\":\"" + largeContent + "\"}\n\n"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
		_, _ = reader.Read()
		_ = reader.Close()
	}
}

func BenchmarkRetryPolicy_ShouldRetry(b *testing.B) {
	policy := DefaultRetryPolicy()
	statusCodes := []int{200, 400, 429, 500, 502, 503, 504}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, code := range statusCodes {
			policy.ShouldRetry(code)
		}
	}
}

func BenchmarkRetryPolicy_GetDelay(b *testing.B) {
	policy := DefaultRetryPolicy()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for attempt := 1; attempt <= policy.MaxRetries; attempt++ {
			policy.GetDelay(attempt)
		}
	}
}

func BenchmarkValidateURL_AllowedHosts(b *testing.B) {
	opts := DefaultURLValidationOptions()
	urls := []string{
		"https://api.openai.com/v1/chat/completions",
		"https://api.anthropic.com/v1/messages",
		"https://generativelanguage.googleapis.com/v1/models",
		"https://api.together.xyz/v1/chat/completions",
		"https://api.featherless.ai/v1/chat/completions",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, url := range urls {
			_ = ValidateURL(url, opts)
		}
	}
}

func BenchmarkValidateURL_StrictMode(b *testing.B) {
	opts := StrictURLValidationOptions()
	urls := []string{
		"https://api.openai.com/v1/chat/completions",
		"https://api.anthropic.com/v1/messages",
		"https://unknown-api.com/v1/endpoint",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, url := range urls {
			_ = ValidateURL(url, opts)
		}
	}
}

func BenchmarkSanitizeModelName(b *testing.B) {
	modelNames := []string{
		"gpt-4",
		"claude-sonnet-4-20250514",
		"../../../etc/passwd",
		"meta-llama/Llama-3.3-70B",
		"model\x00with\x00nulls",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, name := range modelNames {
			SanitizeModelName(name)
		}
	}
}

func BenchmarkAPIError_Error(b *testing.B) {
	err := &APIError{
		StatusCode: 429,
		Message:    "Rate limit exceeded",
		Type:       "rate_limit_error",
		Code:       "rate_limit_exceeded",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}

func BenchmarkAPIError_IsRetryable(b *testing.B) {
	errors := []*APIError{
		{StatusCode: 200},
		{StatusCode: 400},
		{StatusCode: 429},
		{StatusCode: 500},
		{StatusCode: 503},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, err := range errors {
			err.IsRetryable()
		}
	}
}

func BenchmarkNewClient(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewClient()
	}
}

func BenchmarkNewClient_WithOptions(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewClient(
			WithTimeout(30000000000), // 30 seconds in nanoseconds
			WithRetryPolicy(DefaultRetryPolicy()),
		)
	}
}

// Memory allocation benchmarks

func BenchmarkSSEReader_Allocations(b *testing.B) {
	data := strings.Repeat("data: hello world\n\n", 10)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
		for {
			_, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
		}
		_ = reader.Close()
	}
}

func BenchmarkValidateURL_Allocations(b *testing.B) {
	opts := DefaultURLValidationOptions()
	url := "https://api.openai.com/v1/chat/completions"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateURL(url, opts)
	}
}

// Throughput benchmarks

func BenchmarkSSEReader_Throughput(b *testing.B) {
	// Generate 1MB of SSE data
	var buf bytes.Buffer
	chunk := `data: {"id":"123","content":"Lorem ipsum dolor sit amet, consectetur adipiscing elit."}` + "\n\n"
	for buf.Len() < 1024*1024 {
		buf.WriteString(chunk)
	}
	data := buf.String()
	dataLen := int64(len(data))

	b.SetBytes(dataLen)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
		for {
			_, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
		}
		_ = reader.Close()
	}
}
