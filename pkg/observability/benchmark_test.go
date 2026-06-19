package observability

import (
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

// Benchmark LogEntry creation (common operation in logging middleware)
func BenchmarkLogEntry_Creation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &LogEntry{
			RequestID: "req_12345678",
			Timestamp: time.Now(),
			Provider:  llms.ProviderOpenAI,
			Model:     "gpt-4",
			Operation: "GenerateContent",
		}
	}
}
