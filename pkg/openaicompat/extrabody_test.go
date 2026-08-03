package openaicompat

import (
	"encoding/json"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestChatCompletionRequest_ExtraBody(t *testing.T) {
	tests := []struct {
		name       string
		extraBody  map[string]any
		wantFields map[string]any
	}{
		{
			name: "adapter_id is added at top level",
			extraBody: map[string]any{
				"adapter_id": "predibase/sql-lora",
			},
			wantFields: map[string]any{
				"adapter_id": "predibase/sql-lora",
			},
		},
		{
			name: "multiple extra fields",
			extraBody: map[string]any{
				"adapter_id":     "my-adapter",
				"adapter_source": "hub",
				"custom_param":   123,
			},
			wantFields: map[string]any{
				"adapter_id":     "my-adapter",
				"adapter_source": "hub",
				"custom_param":   float64(123), // JSON numbers become float64
			},
		},
		{
			name:       "empty extra body",
			extraBody:  nil,
			wantFields: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ChatCompletionRequest{
				Model: "test-model",
				Messages: []ChatMessage{
					{Role: "user", ContentValue: "Hello"},
				},
				ExtraBody: tt.extraBody,
			}

			// Marshal to JSON
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			// Unmarshal to map to check structure
			var result map[string]any
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("Failed to unmarshal result: %v", err)
			}

			// Verify standard fields are present
			if result["model"] != "test-model" {
				t.Errorf("Expected model 'test-model', got %v", result["model"])
			}

			// Verify extra body fields are at top level (not nested)
			for key, want := range tt.wantFields {
				got, exists := result[key]
				if !exists {
					t.Errorf("Expected field '%s' at top level, but it's missing", key)
					continue
				}
				if got != want {
					t.Errorf("Field '%s': expected %v (%T), got %v (%T)", key, want, want, got, got)
				}
			}

			// Verify there's no "extra_body" nested field
			if _, exists := result["extra_body"]; exists {
				t.Error("ExtraBody should be flattened, not nested under 'extra_body'")
			}
		})
	}
}

func TestBuildChatRequest_WithExtraBody(t *testing.T) {
	messages := []llms.Message{
		{Role: "user", Content: "Hello"},
	}

	opts := llms.ApplyOptions(
		llms.WithAdapterID("test-adapter"),
		llms.WithExtraBodyParam("custom_key", "custom_value"),
	)

	req := BuildChatRequest("test-model", messages, opts, false)

	// Verify ExtraBody is set
	if req.ExtraBody == nil {
		t.Fatal("ExtraBody should not be nil")
	}

	if req.ExtraBody["adapter_id"] != "test-adapter" {
		t.Errorf("Expected adapter_id 'test-adapter', got %v", req.ExtraBody["adapter_id"])
	}

	if req.ExtraBody["custom_key"] != "custom_value" {
		t.Errorf("Expected custom_key 'custom_value', got %v", req.ExtraBody["custom_key"])
	}
}

func TestWithExtraBody_Options(t *testing.T) {
	t.Run("WithExtraBody sets entire map", func(t *testing.T) {
		opts := llms.ApplyOptions(
			llms.WithExtraBody(map[string]any{
				"key1": "value1",
				"key2": 123,
			}),
		)

		if opts.ExtraBody["key1"] != "value1" {
			t.Errorf("Expected key1 'value1', got %v", opts.ExtraBody["key1"])
		}
		if opts.ExtraBody["key2"] != 123 {
			t.Errorf("Expected key2 123, got %v", opts.ExtraBody["key2"])
		}
	})

	t.Run("WithExtraBodyParam adds single param", func(t *testing.T) {
		opts := llms.ApplyOptions(
			llms.WithExtraBodyParam("adapter_id", "my-adapter"),
		)

		if opts.ExtraBody["adapter_id"] != "my-adapter" {
			t.Errorf("Expected adapter_id 'my-adapter', got %v", opts.ExtraBody["adapter_id"])
		}
	})

	t.Run("WithAdapterID convenience function", func(t *testing.T) {
		opts := llms.ApplyOptions(
			llms.WithAdapterID("predibase/sql-lora"),
		)

		if opts.ExtraBody["adapter_id"] != "predibase/sql-lora" {
			t.Errorf("Expected adapter_id 'predibase/sql-lora', got %v", opts.ExtraBody["adapter_id"])
		}
	})

	t.Run("Multiple WithExtraBodyParam calls merge", func(t *testing.T) {
		opts := llms.ApplyOptions(
			llms.WithExtraBodyParam("key1", "value1"),
			llms.WithExtraBodyParam("key2", "value2"),
		)

		if opts.ExtraBody["key1"] != "value1" {
			t.Errorf("Expected key1 'value1', got %v", opts.ExtraBody["key1"])
		}
		if opts.ExtraBody["key2"] != "value2" {
			t.Errorf("Expected key2 'value2', got %v", opts.ExtraBody["key2"])
		}
	})
}
