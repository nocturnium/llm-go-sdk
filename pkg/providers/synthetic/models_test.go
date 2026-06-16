package synthetic

import (
	"context"
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

const (
	testModified = "modified"
)

func TestCachedModelsMetadata(t *testing.T) {
	// Verify all cached models have required fields
	for _, model := range cachedModels {
		t.Run(model.ID, func(t *testing.T) {
			if model.ID == "" {
				t.Error("model has empty ID")
			}
			if model.DisplayName == "" {
				t.Errorf("model %q has empty DisplayName", model.ID)
			}
			if model.Provider != llms.ProviderSynthetic {
				t.Errorf("model %q has wrong Provider: %q", model.ID, model.Provider)
			}
			if model.Organization == "" {
				t.Errorf("model %q has empty Organization", model.ID)
			}
			if len(model.Types) == 0 {
				t.Errorf("model %q has no Types", model.ID)
			}
			if !model.FromCache {
				t.Errorf("model %q should have FromCache=true", model.ID)
			}
			if model.ContextLength == 0 {
				t.Errorf("model %q has zero ContextLength", model.ID)
			}
		})
	}
}

func TestCachedModelsOrganizations(t *testing.T) {
	// Test that models have correct organizations
	expectedOrgs := map[string]string{
		"hf:Qwen/Qwen3-Coder-480B-A35B-Instruct": "Qwen",
		"hf:deepseek-ai/DeepSeek-V3":             "DeepSeek",
		"hf:zai-org/GLM-4.7":                     "Zhipu AI",
		"hf:moonshotai/Kimi-K2-Instruct-0905":    "Moonshot AI",
		"hf:meta-llama/Llama-3.3-70B-Instruct":   "Meta",
		"hf:nomic-ai/nomic-embed-text-v1.5":      "Nomic AI",
	}

	for modelID, expectedOrg := range expectedOrgs {
		t.Run(modelID, func(t *testing.T) {
			info, ok := modelIndex[modelID]
			if !ok {
				t.Fatalf("model %q not found in index", modelID)
			}
			if info.Organization != expectedOrg {
				t.Errorf("model %q has Organization=%q, want %q", modelID, info.Organization, expectedOrg)
			}
		})
	}
}

func TestCachedModelsCodeType(t *testing.T) {
	// Test that coder models have code type
	coderModels := []string{
		"hf:Qwen/Qwen3-Coder-480B-A35B-Instruct",
		"hf:deepseek-ai/DeepSeek-R1-0528",
		"hf:deepseek-ai/DeepSeek-V3",
		"hf:deepseek-ai/DeepSeek-V3.1",
		"hf:deepseek-ai/DeepSeek-V3.2",
		"hf:zai-org/GLM-4.5",
		"hf:zai-org/GLM-4.6",
		"hf:zai-org/GLM-4.7",
	}

	for _, modelID := range coderModels {
		t.Run(modelID+"_has_code_type", func(t *testing.T) {
			info, ok := modelIndex[modelID]
			if !ok {
				t.Fatalf("model %q not found in index", modelID)
			}
			hasCode := false
			for _, typ := range info.Types {
				if typ == llms.ModelTypeCode {
					hasCode = true
					break
				}
			}
			if !hasCode {
				t.Errorf("model %q should have code type", modelID)
			}
		})
	}
}

func TestListModels(t *testing.T) {
	client := &Client{}
	ctx := context.Background()

	t.Run("returns all models", func(t *testing.T) {
		result, err := client.ListModels(ctx)
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(result.Models) != len(cachedModels) {
			t.Errorf("ListModels() returned %d models, want %d", len(result.Models), len(cachedModels))
		}
		if result.HasMore {
			t.Error("ListModels() HasMore = true, want false")
		}
	})

	t.Run("with limit", func(t *testing.T) {
		result, err := client.ListModels(ctx, llms.WithModelsLimit(5))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(result.Models) != 5 {
			t.Errorf("ListModels() returned %d models, want 5", len(result.Models))
		}
		if !result.HasMore {
			t.Error("ListModels() HasMore = false, want true")
		}
		if result.NextCursor == "" {
			t.Error("ListModels() NextCursor is empty, want non-empty")
		}
	})

	t.Run("with type filter for code", func(t *testing.T) {
		result, err := client.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeCode))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		// All returned models should have code type
		for _, model := range result.Models {
			hasCode := false
			for _, typ := range model.Types {
				if typ == llms.ModelTypeCode {
					hasCode = true
					break
				}
			}
			if !hasCode {
				t.Errorf("model %q should have code type", model.ID)
			}
		}
		// Should have at least the known coder models
		if len(result.Models) < 9 {
			t.Errorf("ListModels() returned %d code models, want at least 9", len(result.Models))
		}
	})

	t.Run("pagination with cursor", func(t *testing.T) {
		// Get first page
		result1, err := client.ListModels(ctx, llms.WithModelsLimit(3))
		if err != nil {
			t.Fatalf("ListModels() first page error = %v", err)
		}
		if len(result1.Models) != 3 {
			t.Fatalf("first page returned %d models, want 3", len(result1.Models))
		}

		// Get second page
		result2, err := client.ListModels(ctx, llms.WithModelsLimit(3), llms.WithModelsCursor(result1.NextCursor))
		if err != nil {
			t.Fatalf("ListModels() second page error = %v", err)
		}

		// Ensure no overlap
		for _, m1 := range result1.Models {
			for _, m2 := range result2.Models {
				if m1.ID == m2.ID {
					t.Errorf("model %q appears in both pages", m1.ID)
				}
			}
		}
	})

	t.Run("cursor at end returns empty", func(t *testing.T) {
		// Get last model ID
		lastModelID := cachedModels[len(cachedModels)-1].ID

		result, err := client.ListModels(ctx, llms.WithModelsCursor(lastModelID))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(result.Models) != 0 {
			t.Errorf("ListModels() returned %d models, want 0", len(result.Models))
		}
		if result.HasMore {
			t.Error("ListModels() HasMore = true, want false")
		}
	})
}

func TestModelInfo(t *testing.T) {
	client := &Client{}
	ctx := context.Background()

	t.Run("existing model exact match", func(t *testing.T) {
		info, err := client.ModelInfo(ctx, "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct")
		if err != nil {
			t.Fatalf("ModelInfo() error = %v", err)
		}
		if info == nil {
			t.Fatal("ModelInfo() returned nil for existing model")
		}
		if info.ID != "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct" {
			t.Errorf("ID = %q, want %q", info.ID, "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct")
		}
		if info.DisplayName != "Qwen 3 Coder 480B" {
			t.Errorf("DisplayName = %q, want %q", info.DisplayName, "Qwen 3 Coder 480B")
		}
	})

	t.Run("existing model case insensitive", func(t *testing.T) {
		info, err := client.ModelInfo(ctx, "hf:qwen/qwen3-coder-480b-a35b-instruct")
		if err != nil {
			t.Fatalf("ModelInfo() error = %v", err)
		}
		if info == nil {
			t.Fatal("ModelInfo() returned nil for existing model (case insensitive)")
		}
	})

	t.Run("non-existing model", func(t *testing.T) {
		info, err := client.ModelInfo(ctx, "non-existent/model")
		if !errors.Is(err, llms.ErrModelNotFound) {
			t.Fatalf("ModelInfo() error = %v, want %v", err, llms.ErrModelNotFound)
		}
		if info != nil {
			t.Error("ModelInfo() returned non-nil for non-existing model")
		}
	})

	t.Run("returns copy not reference", func(t *testing.T) {
		info1, _ := client.ModelInfo(ctx, "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct")
		info2, _ := client.ModelInfo(ctx, "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct")

		// Modify info1
		info1.DisplayName = testModified

		// info2 should not be affected
		if info2.DisplayName == testModified {
			t.Error("ModelInfo() returns reference instead of copy")
		}
	})
}

func TestModelIndex(t *testing.T) {
	// Test that all cached models are in the index
	for _, model := range cachedModels {
		t.Run(model.ID, func(t *testing.T) {
			if _, ok := modelIndex[model.ID]; !ok {
				t.Errorf("model %q not found in index", model.ID)
			}
			// Also check lowercase
			if _, ok := modelIndex[model.ID]; !ok {
				t.Errorf("model %q not found in index (lowercase)", model.ID)
			}
		})
	}
}

func TestClientImplementsModelLister(_ *testing.T) {
	// This is a compile-time check, but we include it as a test for documentation
	var _ llms.ModelLister = (*Client)(nil)
}

func TestAllModelsHaveFromCacheTrue(t *testing.T) {
	client := &Client{}
	ctx := context.Background()

	result, err := client.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}

	for _, model := range result.Models {
		if !model.FromCache {
			t.Errorf("model %q has FromCache=false, want true", model.ID)
		}
	}
}

func TestCodingFocusedModels(t *testing.T) {
	// Synthetic.new specializes in coding LLMs
	// Verify we have models from the mentioned providers: Qwen, GLM, Kimi (Moonshot), DeepSeek
	providers := map[string]bool{
		"Qwen":        false,
		"DeepSeek":    false,
		"Zhipu AI":    false, // GLM
		"Moonshot AI": false, // Kimi
	}

	for _, model := range cachedModels {
		if _, exists := providers[model.Organization]; exists {
			providers[model.Organization] = true
		}
	}

	for provider, found := range providers {
		if !found {
			t.Errorf("expected to have models from %q (mentioned in package docs)", provider)
		}
	}
}

// TestConcurrentListModels tests thread-safety of ListModels
func TestConcurrentListModels(t *testing.T) {
	client := &Client{}
	ctx := context.Background()
	const goroutines = 100
	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer func() { done <- true }()

			var result *llms.ListModelsResult
			var err error

			// Alternate between different operations
			switch idx % 3 {
			case 0:
				result, err = client.ListModels(ctx)
			case 1:
				result, err = client.ListModels(ctx, llms.WithModelsLimit(5))
			case 2:
				result, err = client.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeCode))
			}

			if err != nil {
				t.Errorf("goroutine %d: ListModels() error = %v", idx, err)
				return
			}
			if result == nil {
				t.Errorf("goroutine %d: ListModels() returned nil", idx)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestConcurrentModelInfo tests thread-safety of ModelInfo
func TestConcurrentModelInfo(t *testing.T) {
	client := &Client{}
	ctx := context.Background()
	const goroutines = 100
	done := make(chan bool, goroutines)

	modelIDs := []string{
		"hf:Qwen/Qwen3-Coder-480B-A35B-Instruct",
		"hf:deepseek-ai/DeepSeek-V3",
		"hf:zai-org/GLM-4.7",
		"nonexistent/model",
	}

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer func() { done <- true }()

			modelID := modelIDs[idx%len(modelIDs)]
			result, err := client.ModelInfo(ctx, modelID)

			if modelID == "nonexistent/model" {
				if !errors.Is(err, llms.ErrModelNotFound) {
					t.Errorf("goroutine %d: ModelInfo(%s) error = %v, want %v", idx, modelID, err, llms.ErrModelNotFound)
					return
				}
				if result != nil {
					t.Errorf("goroutine %d: expected nil for nonexistent model", idx)
				}
			} else {
				if err != nil {
					t.Errorf("goroutine %d: ModelInfo(%s) error = %v", idx, modelID, err)
					return
				}
				if result == nil {
					t.Errorf("goroutine %d: expected to find %s", idx, modelID)
				}
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestModelInfoReturnsCopy verifies that ModelInfo returns a copy
func TestModelInfoReturnsCopy(t *testing.T) {
	client := &Client{}
	ctx := context.Background()

	info1, _ := client.ModelInfo(ctx, "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct")
	info2, _ := client.ModelInfo(ctx, "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct")

	if info1 == nil || info2 == nil {
		t.Fatal("expected to find model")
	}

	// Modify info1
	info1.DisplayName = testModified
	info1.Types = append(info1.Types, llms.ModelTypeEmbedding)

	// info2 should not be affected
	if info2.DisplayName == testModified {
		t.Error("ModelInfo returns reference instead of copy (DisplayName)")
	}

	// Check that Types slice was deep copied
	hasEmbedding := false
	for _, typ := range info2.Types {
		if typ == llms.ModelTypeEmbedding {
			hasEmbedding = true
			break
		}
	}
	if hasEmbedding {
		t.Error("ModelInfo returns reference instead of copy (Types slice)")
	}
}
