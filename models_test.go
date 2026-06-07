package llms

import (
	"testing"
	"time"
)

// Test data
var testModels = []ModelInfo{
	{
		ID:            "gpt-4o",
		DisplayName:   "GPT-4o",
		Provider:      ProviderOpenAI,
		Types:         []ModelType{ModelTypeChat, ModelTypeVision},
		ContextLength: 128000,
		MaxOutput:     16384,
		Organization:  "OpenAI",
		Pricing:       &ModelPricing{Input: 2.50, Output: 10.00},
		CreatedAt:     time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
	},
	{
		ID:            "gpt-4o-mini",
		DisplayName:   "GPT-4o Mini",
		Provider:      ProviderOpenAI,
		Types:         []ModelType{ModelTypeChat, ModelTypeVision},
		ContextLength: 128000,
		MaxOutput:     16384,
		Organization:  "OpenAI",
		Pricing:       &ModelPricing{Input: 0.15, Output: 0.60},
		CreatedAt:     time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
	},
	{
		ID:            "claude-3-opus-20240229",
		DisplayName:   "Claude 3 Opus",
		Provider:      ProviderAnthropic,
		Types:         []ModelType{ModelTypeChat, ModelTypeVision},
		ContextLength: 200000,
		MaxOutput:     4096,
		Organization:  "Anthropic",
		Pricing:       &ModelPricing{Input: 15.00, Output: 75.00},
		CreatedAt:     time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
	},
	{
		ID:            "claude-3-haiku-20240307",
		DisplayName:   "Claude 3 Haiku",
		Provider:      ProviderAnthropic,
		Types:         []ModelType{ModelTypeChat, ModelTypeVision},
		ContextLength: 200000,
		MaxOutput:     4096,
		Organization:  "Anthropic",
		Pricing:       &ModelPricing{Input: 0.25, Output: 1.25},
		CreatedAt:     time.Date(2024, 3, 7, 0, 0, 0, 0, time.UTC),
	},
	{
		ID:            "text-embedding-3-large",
		DisplayName:   "Text Embedding 3 Large",
		Provider:      ProviderOpenAI,
		Types:         []ModelType{ModelTypeEmbedding},
		ContextLength: 8191,
		Organization:  "OpenAI",
		Pricing:       &ModelPricing{Input: 0.13},
		CreatedAt:     time.Date(2024, 1, 25, 0, 0, 0, 0, time.UTC),
	},
	{
		ID:            "meta-llama/Llama-3.3-70B-Instruct-Turbo",
		DisplayName:   "Llama 3.3 70B Instruct Turbo",
		Provider:      ProviderTogetherAI,
		Types:         []ModelType{ModelTypeChat, ModelTypeCode},
		ContextLength: 131072,
		Organization:  "Meta",
		Pricing:       &ModelPricing{Input: 0.88, Output: 0.88},
		CreatedAt:     time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
	},
	{
		ID:          "no-pricing-model",
		DisplayName: "Model Without Pricing",
		Provider:    ProviderFeatherless,
		Types:       []ModelType{ModelTypeChat},
		FromCache:   true,
	},
}

// TestModelInfo tests ModelInfo methods
func TestModelInfo_HasType(t *testing.T) {
	model := testModels[0] // gpt-4o with Chat and Vision

	tests := []struct {
		modelType ModelType
		expected  bool
	}{
		{ModelTypeChat, true},
		{ModelTypeVision, true},
		{ModelTypeEmbedding, false},
		{ModelTypeCode, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.modelType), func(t *testing.T) {
			if got := model.HasType(tt.modelType); got != tt.expected {
				t.Errorf("HasType(%s) = %v, want %v", tt.modelType, got, tt.expected)
			}
		})
	}
}

func TestModelInfo_IsChat(t *testing.T) {
	chatModel := testModels[0]
	embeddingModel := testModels[4]

	if !chatModel.IsChat() {
		t.Error("gpt-4o should be a chat model")
	}
	if embeddingModel.IsChat() {
		t.Error("text-embedding-3-large should not be a chat model")
	}
}

func TestModelInfo_IsEmbedding(t *testing.T) {
	chatModel := testModels[0]
	embeddingModel := testModels[4]

	if chatModel.IsEmbedding() {
		t.Error("gpt-4o should not be an embedding model")
	}
	if !embeddingModel.IsEmbedding() {
		t.Error("text-embedding-3-large should be an embedding model")
	}
}

func TestModelInfo_IsVision(t *testing.T) {
	visionModel := testModels[0]   // gpt-4o
	noVisionModel := testModels[5] // llama

	if !visionModel.IsVision() {
		t.Error("gpt-4o should be a vision model")
	}
	if noVisionModel.IsVision() {
		t.Error("llama should not be a vision model")
	}
}

// TestListModelsOptions tests functional options
func TestListModelsOptions(t *testing.T) {
	t.Run("WithModelTypes", func(t *testing.T) {
		opts := ApplyListModelsOptions(WithModelTypes(ModelTypeChat, ModelTypeVision))
		if len(opts.Types) != 2 {
			t.Errorf("expected 2 types, got %d", len(opts.Types))
		}
		if opts.Types[0] != ModelTypeChat {
			t.Errorf("expected first type to be chat, got %s", opts.Types[0])
		}
	})

	t.Run("WithDedicatedModels", func(t *testing.T) {
		opts := ApplyListModelsOptions(WithDedicatedModels())
		if !opts.Dedicated {
			t.Error("expected Dedicated to be true")
		}
	})

	t.Run("WithModelsLimit", func(t *testing.T) {
		opts := ApplyListModelsOptions(WithModelsLimit(50))
		if opts.Limit != 50 {
			t.Errorf("expected limit 50, got %d", opts.Limit)
		}
	})

	t.Run("WithModelsCursor", func(t *testing.T) {
		opts := ApplyListModelsOptions(WithModelsCursor("next-page-token"))
		if opts.Cursor != "next-page-token" {
			t.Errorf("expected cursor 'next-page-token', got %s", opts.Cursor)
		}
	})

	t.Run("combined options", func(t *testing.T) {
		opts := ApplyListModelsOptions(
			WithModelTypes(ModelTypeChat),
			WithDedicatedModels(),
			WithModelsLimit(100),
			WithModelsCursor("abc123"),
		)
		if len(opts.Types) != 1 || opts.Types[0] != ModelTypeChat {
			t.Error("types not set correctly")
		}
		if !opts.Dedicated {
			t.Error("dedicated not set correctly")
		}
		if opts.Limit != 100 {
			t.Error("limit not set correctly")
		}
		if opts.Cursor != "abc123" {
			t.Error("cursor not set correctly")
		}
	})
}

// TestFilterModelsByType tests type filtering
func TestFilterModelsByType(t *testing.T) {
	t.Run("filter by chat", func(t *testing.T) {
		result := FilterModelsByType(testModels, ModelTypeChat)
		// All models except embedding should have chat
		if len(result) != 6 {
			t.Errorf("expected 6 chat models, got %d", len(result))
		}
	})

	t.Run("filter by embedding", func(t *testing.T) {
		result := FilterModelsByType(testModels, ModelTypeEmbedding)
		if len(result) != 1 {
			t.Errorf("expected 1 embedding model, got %d", len(result))
		}
		if result[0].ID != "text-embedding-3-large" {
			t.Errorf("expected text-embedding-3-large, got %s", result[0].ID)
		}
	})

	t.Run("filter by vision", func(t *testing.T) {
		result := FilterModelsByType(testModels, ModelTypeVision)
		if len(result) != 4 {
			t.Errorf("expected 4 vision models, got %d", len(result))
		}
	})

	t.Run("filter by code", func(t *testing.T) {
		result := FilterModelsByType(testModels, ModelTypeCode)
		if len(result) != 1 {
			t.Errorf("expected 1 code model, got %d", len(result))
		}
	})

	t.Run("filter by multiple types (OR)", func(t *testing.T) {
		result := FilterModelsByType(testModels, ModelTypeEmbedding, ModelTypeCode)
		if len(result) != 2 {
			t.Errorf("expected 2 models (embedding OR code), got %d", len(result))
		}
	})

	t.Run("empty types returns all", func(t *testing.T) {
		result := FilterModelsByType(testModels)
		if len(result) != len(testModels) {
			t.Errorf("expected all models, got %d", len(result))
		}
	})
}

func TestFilterChatModels(t *testing.T) {
	result := FilterChatModels(testModels)
	for _, m := range result {
		if !m.HasType(ModelTypeChat) {
			t.Errorf("model %s does not have chat type", m.ID)
		}
	}
}

func TestFilterEmbeddingModels(t *testing.T) {
	result := FilterEmbeddingModels(testModels)
	if len(result) != 1 {
		t.Errorf("expected 1 embedding model, got %d", len(result))
	}
}

// TestFilterModelsByProvider tests provider filtering
func TestFilterModelsByProvider(t *testing.T) {
	t.Run("filter OpenAI", func(t *testing.T) {
		result := FilterModelsByProvider(testModels, ProviderOpenAI)
		if len(result) != 3 {
			t.Errorf("expected 3 OpenAI models, got %d", len(result))
		}
	})

	t.Run("filter Anthropic", func(t *testing.T) {
		result := FilterModelsByProvider(testModels, ProviderAnthropic)
		if len(result) != 2 {
			t.Errorf("expected 2 Anthropic models, got %d", len(result))
		}
	})

	t.Run("filter TogetherAI", func(t *testing.T) {
		result := FilterModelsByProvider(testModels, ProviderTogetherAI)
		if len(result) != 1 {
			t.Errorf("expected 1 TogetherAI model, got %d", len(result))
		}
	})
}

// TestFilterModelsByOrganization tests organization filtering
func TestFilterModelsByOrganization(t *testing.T) {
	t.Run("filter by OpenAI", func(t *testing.T) {
		result := FilterModelsByOrganization(testModels, "OpenAI")
		if len(result) != 3 {
			t.Errorf("expected 3 OpenAI org models, got %d", len(result))
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		result := FilterModelsByOrganization(testModels, "openai")
		if len(result) != 3 {
			t.Errorf("expected 3 OpenAI org models (case insensitive), got %d", len(result))
		}
	})

	t.Run("filter by Meta", func(t *testing.T) {
		result := FilterModelsByOrganization(testModels, "Meta")
		if len(result) != 1 {
			t.Errorf("expected 1 Meta model, got %d", len(result))
		}
	})
}

// TestFilterModelsByMinContext tests context length filtering
func TestFilterModelsByMinContext(t *testing.T) {
	t.Run("min 100k context", func(t *testing.T) {
		result := FilterModelsByMinContext(testModels, 100000)
		if len(result) != 5 {
			t.Errorf("expected 5 models with 100k+ context, got %d", len(result))
		}
	})

	t.Run("min 200k context", func(t *testing.T) {
		result := FilterModelsByMinContext(testModels, 200000)
		if len(result) != 2 {
			t.Errorf("expected 2 models with 200k context, got %d", len(result))
		}
	})
}

// TestSearchModels tests search functionality
func TestSearchModels(t *testing.T) {
	t.Run("search by ID substring", func(t *testing.T) {
		result := SearchModels(testModels, "gpt")
		if len(result) != 2 {
			t.Errorf("expected 2 gpt models, got %d", len(result))
		}
	})

	t.Run("search by display name", func(t *testing.T) {
		result := SearchModels(testModels, "Opus")
		if len(result) != 1 {
			t.Errorf("expected 1 Opus model, got %d", len(result))
		}
	})

	t.Run("case insensitive search", func(t *testing.T) {
		result := SearchModels(testModels, "CLAUDE")
		if len(result) != 2 {
			t.Errorf("expected 2 Claude models, got %d", len(result))
		}
	})

	t.Run("no matches", func(t *testing.T) {
		result := SearchModels(testModels, "nonexistent")
		if len(result) != 0 {
			t.Errorf("expected 0 matches, got %d", len(result))
		}
	})
}

// TestSortModelsByContextLength tests context length sorting
func TestSortModelsByContextLength(t *testing.T) {
	t.Run("descending", func(t *testing.T) {
		result := SortModelsByContextLength(testModels, true)
		if result[0].ContextLength < result[len(result)-1].ContextLength {
			t.Error("expected descending order")
		}
		// First should be Claude (200k)
		if result[0].ContextLength != 200000 {
			t.Errorf("expected first model to have 200k context, got %d", result[0].ContextLength)
		}
	})

	t.Run("ascending", func(t *testing.T) {
		result := SortModelsByContextLength(testModels, false)
		if result[0].ContextLength > result[len(result)-1].ContextLength {
			t.Error("expected ascending order")
		}
	})

	t.Run("does not modify original", func(t *testing.T) {
		original := testModels[0].ID
		_ = SortModelsByContextLength(testModels, true)
		if testModels[0].ID != original {
			t.Error("original slice was modified")
		}
	})
}

// TestSortModelsByInputPrice tests price sorting
func TestSortModelsByInputPrice(t *testing.T) {
	t.Run("ascending", func(t *testing.T) {
		result := SortModelsByInputPrice(testModels, false)
		// Models with pricing should come first, sorted by price
		// Model without pricing should be last
		lastWithPricing := -1
		for i, m := range result {
			if m.Pricing != nil {
				lastWithPricing = i
			}
		}
		// Check that model without pricing is after all priced models
		for i := lastWithPricing + 1; i < len(result); i++ {
			if result[i].Pricing != nil {
				t.Error("model with pricing found after model without pricing")
			}
		}
	})

	t.Run("descending", func(t *testing.T) {
		result := SortModelsByInputPrice(testModels, true)
		// Most expensive should be first (Claude Opus at $15)
		if result[0].Pricing == nil || result[0].Pricing.Input != 15.00 {
			t.Errorf("expected Claude Opus ($15) first, got %v", result[0].Pricing)
		}
	})
}

// TestSortModelsByName tests alphabetical sorting
func TestSortModelsByName(t *testing.T) {
	t.Run("ascending", func(t *testing.T) {
		result := SortModelsByName(testModels, false)
		for i := 1; i < len(result); i++ {
			if result[i-1].ID > result[i].ID {
				t.Errorf("not sorted ascending: %s > %s", result[i-1].ID, result[i].ID)
			}
		}
	})

	t.Run("descending", func(t *testing.T) {
		result := SortModelsByName(testModels, true)
		for i := 1; i < len(result); i++ {
			if result[i-1].ID < result[i].ID {
				t.Errorf("not sorted descending: %s < %s", result[i-1].ID, result[i].ID)
			}
		}
	})
}

// TestSortModelsByCreatedAt tests date sorting
func TestSortModelsByCreatedAt(t *testing.T) {
	t.Run("newest first", func(t *testing.T) {
		result := SortModelsByCreatedAt(testModels, true)
		// Llama is newest (Dec 2024)
		if result[0].ID != "meta-llama/Llama-3.3-70B-Instruct-Turbo" {
			t.Errorf("expected Llama first (newest), got %s", result[0].ID)
		}
	})

	t.Run("oldest first", func(t *testing.T) {
		result := SortModelsByCreatedAt(testModels, false)
		// Model without date (zero time) should be first
		// Then text-embedding-3-large (Jan 2024)
		// Find first model with a date set
		for _, m := range result {
			if !m.CreatedAt.IsZero() {
				if m.ID != "text-embedding-3-large" {
					t.Errorf("expected text-embedding-3-large as oldest dated model, got %s", m.ID)
				}
				break
			}
		}
	})
}

// TestFindModelByID tests finding models by ID
func TestFindModelByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		result := FindModelByID(testModels, "gpt-4o")
		if result == nil {
			t.Fatal("expected to find gpt-4o")
		}
		if result.ID != "gpt-4o" {
			t.Errorf("expected gpt-4o, got %s", result.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		result := FindModelByID(testModels, "nonexistent")
		if result != nil {
			t.Error("expected nil for nonexistent model")
		}
	})
}

// TestGroupModelsByProvider tests provider grouping
func TestGroupModelsByProvider(t *testing.T) {
	result := GroupModelsByProvider(testModels)

	if len(result[ProviderOpenAI]) != 3 {
		t.Errorf("expected 3 OpenAI models, got %d", len(result[ProviderOpenAI]))
	}
	if len(result[ProviderAnthropic]) != 2 {
		t.Errorf("expected 2 Anthropic models, got %d", len(result[ProviderAnthropic]))
	}
	if len(result[ProviderTogetherAI]) != 1 {
		t.Errorf("expected 1 TogetherAI model, got %d", len(result[ProviderTogetherAI]))
	}
}

// TestGroupModelsByType tests type grouping
func TestGroupModelsByType(t *testing.T) {
	result := GroupModelsByType(testModels)

	if len(result[ModelTypeChat]) != 6 {
		t.Errorf("expected 6 chat models, got %d", len(result[ModelTypeChat]))
	}
	if len(result[ModelTypeVision]) != 4 {
		t.Errorf("expected 4 vision models, got %d", len(result[ModelTypeVision]))
	}
	if len(result[ModelTypeEmbedding]) != 1 {
		t.Errorf("expected 1 embedding model, got %d", len(result[ModelTypeEmbedding]))
	}
}

// TestGetUniqueOrganizations tests unique org extraction
func TestGetUniqueOrganizations(t *testing.T) {
	result := GetUniqueOrganizations(testModels)

	expected := []string{"Anthropic", "Meta", "OpenAI"}
	if len(result) != len(expected) {
		t.Errorf("expected %d orgs, got %d", len(expected), len(result))
	}

	// Result should be sorted
	for i, org := range expected {
		if result[i] != org {
			t.Errorf("expected %s at index %d, got %s", org, i, result[i])
		}
	}
}

// TestGetUniqueTypes tests unique type extraction
func TestGetUniqueTypes(t *testing.T) {
	result := GetUniqueTypes(testModels)

	// Should have chat, vision, embedding, code
	typeSet := make(map[ModelType]bool)
	for _, t := range result {
		typeSet[t] = true
	}

	if !typeSet[ModelTypeChat] {
		t.Error("expected chat type")
	}
	if !typeSet[ModelTypeVision] {
		t.Error("expected vision type")
	}
	if !typeSet[ModelTypeEmbedding] {
		t.Error("expected embedding type")
	}
	if !typeSet[ModelTypeCode] {
		t.Error("expected code type")
	}
}

// TestApplyModelFilters tests combined filter application
func TestApplyModelFilters(t *testing.T) {
	t.Run("nil options", func(t *testing.T) {
		result := ApplyModelFilters(testModels, nil)
		if len(result) != len(testModels) {
			t.Error("nil options should return all models")
		}
	})

	t.Run("with type filter", func(t *testing.T) {
		opts := &ListModelsOptions{Types: []ModelType{ModelTypeEmbedding}}
		result := ApplyModelFilters(testModels, opts)
		if len(result) != 1 {
			t.Errorf("expected 1 embedding model, got %d", len(result))
		}
	})

	t.Run("with limit", func(t *testing.T) {
		opts := &ListModelsOptions{Limit: 3}
		result := ApplyModelFilters(testModels, opts)
		if len(result) != 3 {
			t.Errorf("expected 3 models, got %d", len(result))
		}
	})

	t.Run("with type and limit", func(t *testing.T) {
		opts := &ListModelsOptions{
			Types: []ModelType{ModelTypeChat},
			Limit: 2,
		}
		result := ApplyModelFilters(testModels, opts)
		if len(result) != 2 {
			t.Errorf("expected 2 models, got %d", len(result))
		}
		for _, m := range result {
			if !m.HasType(ModelTypeChat) {
				t.Errorf("model %s should have chat type", m.ID)
			}
		}
	})
}

// TestConcurrentFilterModelsByType tests thread-safety of FilterModelsByType
func TestConcurrentFilterModelsByType(t *testing.T) {
	const goroutines = 100
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer func() { done <- true }()

			// Alternate between different filter types
			var result []ModelInfo
			switch idx % 4 {
			case 0:
				result = FilterModelsByType(testModels, ModelTypeChat)
			case 1:
				result = FilterModelsByType(testModels, ModelTypeVision)
			case 2:
				result = FilterModelsByType(testModels, ModelTypeEmbedding)
			case 3:
				result = FilterModelsByType(testModels) // No filter
			}

			// Verify result is valid
			if len(result) == 0 && idx%4 != 2 {
				t.Errorf("goroutine %d: expected non-empty result", idx)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestConcurrentSortModels tests thread-safety of sort functions
func TestConcurrentSortModels(t *testing.T) {
	const goroutines = 100
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer func() { done <- true }()

			// Alternate between different sort functions
			switch idx % 4 {
			case 0:
				SortModelsByContextLength(testModels, idx%2 == 0)
			case 1:
				SortModelsByInputPrice(testModels, idx%2 == 0)
			case 2:
				SortModelsByName(testModels, idx%2 == 0)
			case 3:
				SortModelsByCreatedAt(testModels, idx%2 == 0)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// Verify original slice wasn't modified
	if testModels[0].ID != "gpt-4o" {
		t.Error("original slice was modified during concurrent access")
	}
}

// TestConcurrentFindModelByID tests thread-safety of FindModelByID
func TestConcurrentFindModelByID(t *testing.T) {
	const goroutines = 100
	done := make(chan bool, goroutines)

	modelIDs := []string{"gpt-4o", "gpt-4o-mini", "claude-3-opus-20240229", "nonexistent"}

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer func() { done <- true }()

			id := modelIDs[idx%len(modelIDs)]
			result := FindModelByID(testModels, id)

			if id == "nonexistent" {
				if result != nil {
					t.Errorf("goroutine %d: expected nil for nonexistent model", idx)
				}
			} else {
				if result == nil {
					t.Errorf("goroutine %d: expected to find %s", idx, id)
				} else if result.ID != id {
					t.Errorf("goroutine %d: expected %s, got %s", idx, id, result.ID)
				}
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestConcurrentApplyModelFilters tests thread-safety of ApplyModelFilters
func TestConcurrentApplyModelFilters(t *testing.T) {
	const goroutines = 100
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer func() { done <- true }()

			opts := &ListModelsOptions{
				Limit: idx%5 + 1,
			}
			if idx%2 == 0 {
				opts.Types = []ModelType{ModelTypeChat}
			}

			result := ApplyModelFilters(testModels, opts)
			if len(result) == 0 {
				t.Errorf("goroutine %d: expected non-empty result", idx)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestFilterModelsByTypeReturnsCopy verifies that the function returns a copy
func TestFilterModelsByTypeReturnsCopy(t *testing.T) {
	original := make([]ModelInfo, len(testModels))
	copy(original, testModels)

	result := FilterModelsByType(testModels)

	// Modify the result
	if len(result) > 0 {
		result[0].ID = "MODIFIED"
	}

	// Original should not be modified
	if testModels[0].ID != original[0].ID {
		t.Error("FilterModelsByType did not return a copy; original was modified")
	}
}

// TestApplyModelFiltersReturnsCopy verifies that the function returns a copy
func TestApplyModelFiltersReturnsCopy(t *testing.T) {
	original := make([]ModelInfo, len(testModels))
	copy(original, testModels)

	result := ApplyModelFilters(testModels, nil)

	// Modify the result
	if len(result) > 0 {
		result[0].ID = "MODIFIED"
	}

	// Original should not be modified
	if testModels[0].ID != original[0].ID {
		t.Error("ApplyModelFilters did not return a copy; original was modified")
	}
}
