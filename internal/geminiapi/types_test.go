package geminiapi

import (
	"encoding/json"
	"testing"
)

const (
	testUser = "user"
	testStop = "STOP"
)

// float64Ptr returns a pointer to the given float64, for building configs whose
// sampling parameters are *float64 (so an explicit 0.0 is distinguishable from unset).
func float64Ptr(v float64) *float64 { return &v }

func TestGenerateContentRequest_JSON(t *testing.T) {
	req := GenerateContentRequest{
		Contents: []Content{
			{
				Role: "user",
				Parts: []Part{
					{Text: "Hello"},
				},
			},
		},
		GenerationConfig: &GenerationConfig{
			Temperature:     float64Ptr(0.7),
			MaxOutputTokens: 1024,
			TopP:            float64Ptr(0.9),
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded GenerateContentRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(decoded.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(decoded.Contents))
	}
	if decoded.Contents[0].Role != testUser {
		t.Errorf("expected role=user, got %s", decoded.Contents[0].Role)
	}
	if decoded.GenerationConfig.Temperature == nil || *decoded.GenerationConfig.Temperature != 0.7 {
		t.Errorf("expected temperature=0.7, got %v", decoded.GenerationConfig.Temperature)
	}
}

func TestGenerateContentRequest_WithTools(t *testing.T) {
	req := GenerateContentRequest{
		Contents: []Content{
			{Role: "user", Parts: []Part{{Text: "Weather?"}}},
		},
		Tools: []Tool{
			{
				FunctionDeclarations: []FunctionDeclaration{
					{
						Name:        "get_weather",
						Description: "Get weather",
						Parameters: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"location": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded GenerateContentRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(decoded.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(decoded.Tools))
	}
	if len(decoded.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 function, got %d", len(decoded.Tools[0].FunctionDeclarations))
	}
	if decoded.Tools[0].FunctionDeclarations[0].Name != testGetWeather {
		t.Errorf("expected name=get_weather, got %s", decoded.Tools[0].FunctionDeclarations[0].Name)
	}
}

func TestGenerateContentResponse_JSON(t *testing.T) {
	jsonData := `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [{"text": "Hello! How can I help?"}]
			},
			"finishReason": "STOP",
			"index": 0
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 8,
			"totalTokenCount": 18
		}
	}`

	var resp GenerateContentResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(resp.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(resp.Candidates))
	}
	if resp.Candidates[0].FinishReason != testStop {
		t.Errorf("expected finishReason=STOP, got %s", resp.Candidates[0].FinishReason)
	}
	if resp.Candidates[0].Content.Role != "model" {
		t.Errorf("expected role=model, got %s", resp.Candidates[0].Content.Role)
	}
	if resp.UsageMetadata.TotalTokenCount != 18 {
		t.Errorf("expected totalTokenCount=18, got %d", resp.UsageMetadata.TotalTokenCount)
	}
}

func TestGenerateContentResponse_WithFunctionCall(t *testing.T) {
	jsonData := `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [{
					"functionCall": {
						"name": "get_weather",
						"args": {"location": "Boston"}
					}
				}]
			},
			"finishReason": "STOP"
		}]
	}`

	var resp GenerateContentResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(resp.Candidates[0].Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(resp.Candidates[0].Content.Parts))
	}

	fc := resp.Candidates[0].Content.Parts[0].FunctionCall
	if fc == nil {
		t.Fatal("expected functionCall to be set")
	}
	if fc.Name != "get_weather" {
		t.Errorf("expected name=get_weather, got %s", fc.Name)
	}
	if fc.Args["location"] != "Boston" {
		t.Errorf("expected location=Boston, got %v", fc.Args["location"])
	}
}

func TestPart_FunctionResponse(t *testing.T) {
	part := Part{
		FunctionResponse: &FunctionResponse{
			Name: "get_weather",
			Response: map[string]any{
				"temperature": 72,
				"condition":   "sunny",
			},
		},
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Part
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.FunctionResponse == nil {
		t.Fatal("expected functionResponse to be set")
	}
	if decoded.FunctionResponse.Name != "get_weather" {
		t.Errorf("expected name=get_weather, got %s", decoded.FunctionResponse.Name)
	}
}

func TestStreamChunk_JSON(t *testing.T) {
	jsonData := `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [{"text": "Hello"}]
			},
			"finishReason": ""
		}]
	}`

	var chunk StreamChunk
	if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(chunk.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(chunk.Candidates))
	}
	if chunk.Candidates[0].Content.Parts[0].Text != "Hello" {
		t.Errorf("expected text=Hello, got %s", chunk.Candidates[0].Content.Parts[0].Text)
	}
}

func TestGenerationConfig_JSON(t *testing.T) {
	config := GenerationConfig{
		Temperature:      float64Ptr(0.8),
		TopP:             float64Ptr(0.95),
		TopK:             40,
		MaxOutputTokens:  2048,
		StopSequences:    []string{"\n\n"},
		ResponseMimeType: "application/json",
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded GenerationConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Temperature == nil || *decoded.Temperature != 0.8 {
		t.Errorf("expected temperature=0.8, got %v", decoded.Temperature)
	}
	if decoded.TopK != 40 {
		t.Errorf("expected topK=40, got %d", decoded.TopK)
	}
	if decoded.ResponseMimeType != "application/json" {
		t.Errorf("expected responseMimeType=application/json, got %s", decoded.ResponseMimeType)
	}
}

func TestSafetySetting_JSON(t *testing.T) {
	setting := SafetySetting{
		Category:  "HARM_CATEGORY_HARASSMENT",
		Threshold: "BLOCK_MEDIUM_AND_ABOVE",
	}

	data, err := json.Marshal(setting)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SafetySetting
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Category != "HARM_CATEGORY_HARASSMENT" {
		t.Errorf("expected category=HARM_CATEGORY_HARASSMENT, got %s", decoded.Category)
	}
}

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name     string
		parts    []Part
		expected string
	}{
		{
			name:     "empty",
			parts:    []Part{},
			expected: "",
		},
		{
			name: "single text",
			parts: []Part{
				{Text: "Hello"},
			},
			expected: "Hello",
		},
		{
			name: "multiple text",
			parts: []Part{
				{Text: "Hello "},
				{Text: "world"},
			},
			expected: "Hello world",
		},
		{
			name: "mixed with function call",
			parts: []Part{
				{Text: "Let me check"},
				{FunctionCall: &FunctionCall{Name: "get_weather"}},
			},
			expected: "Let me check",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractTextContent(tc.parts)
			if result != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestFunctionCallToJSON(t *testing.T) {
	tests := []struct {
		name     string
		fc       *FunctionCall
		expected string
	}{
		{
			name:     "nil",
			fc:       nil,
			expected: "{}",
		},
		{
			name:     "nil args",
			fc:       &FunctionCall{Name: "test"},
			expected: "{}",
		},
		{
			name: "with args",
			fc: &FunctionCall{
				Name: "get_weather",
				Args: map[string]any{"location": "Boston"},
			},
			expected: `{"location":"Boston"}`,
		},
		{
			name: "complex args",
			fc: &FunctionCall{
				Name: "search",
				Args: map[string]any{
					"query": "weather",
					"limit": float64(10),
				},
			},
			expected: "", // Will check contains
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FunctionCallToJSON(tc.fc)
			if tc.expected != "" && result != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, result)
			}
			// For complex case, just verify it's valid JSON
			if tc.name == "complex args" {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("expected valid JSON, got error: %v", err)
				}
			}
		})
	}
}

func TestGetFinishReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"STOP", "stop"},
		{"stop", "stop"},
		{"MAX_TOKENS", "length"},
		{"max_tokens", "length"},
		{"SAFETY", "content_filter"},
		{"RECITATION", "content_filter"},
		{"OTHER", "other"},
		{"", ""},
	}

	for _, tc := range tests {
		result := GetFinishReason(tc.input)
		if result != tc.expected {
			t.Errorf("GetFinishReason(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
}

func TestHasFunctionCalls(t *testing.T) {
	tests := []struct {
		name     string
		content  *Content
		expected bool
	}{
		{
			name:     "nil content",
			content:  nil,
			expected: false,
		},
		{
			name: "no function calls",
			content: &Content{
				Parts: []Part{{Text: "Hello"}},
			},
			expected: false,
		},
		{
			name: "with function call",
			content: &Content{
				Parts: []Part{
					{FunctionCall: &FunctionCall{Name: "test"}},
				},
			},
			expected: true,
		},
		{
			name: "mixed parts with function call",
			content: &Content{
				Parts: []Part{
					{Text: "Let me check"},
					{FunctionCall: &FunctionCall{Name: "get_weather"}},
				},
			},
			expected: true,
		},
		{
			name: "empty parts",
			content: &Content{
				Parts: []Part{},
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := HasFunctionCalls(tc.content)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}
