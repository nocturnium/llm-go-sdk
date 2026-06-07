package featherless

import (
	"context"
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
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
			if model.Provider != llms.ProviderFeatherless {
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
		"meta-llama/Llama-3.3-70B-Instruct":         "Meta",
		"Qwen/Qwen2.5-72B-Instruct":                 "Qwen",
		"mistralai/Mixtral-8x7B-Instruct-v0.1":      "Mistral AI",
		"deepseek-ai/DeepSeek-V2.5":                 "DeepSeek",
		"microsoft/Phi-3-medium-128k-instruct":      "Microsoft",
		"google/gemma-2-9b-it":                      "Google",
		"CohereForAI/c4ai-command-r-plus":           "Cohere",
		"nvidia/Llama-3.1-Nemotron-70B-Instruct-HF": "NVIDIA",
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

func TestCachedModelsTypes(t *testing.T) {
	// Test that coder models have code type
	coderModels := []string{
		"Qwen/Qwen2.5-Coder-7B-Instruct",
		"Qwen/Qwen2.5-Coder-32B-Instruct",
		"deepseek-ai/DeepSeek-Coder-V2-Instruct",
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
	// Create a client for testing (we can't actually call the API without credentials)
	// But we can test the ListModels method which uses cached data
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

	t.Run("with type filter", func(t *testing.T) {
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
}

func TestModelInfo(t *testing.T) {
	client := &Client{}
	ctx := context.Background()

	t.Run("existing model exact match", func(t *testing.T) {
		info, err := client.ModelInfo(ctx, "meta-llama/Llama-3.3-70B-Instruct")
		if err != nil {
			t.Fatalf("ModelInfo() error = %v", err)
		}
		if info == nil {
			t.Fatal("ModelInfo() returned nil for existing model")
		}
		if info.ID != "meta-llama/Llama-3.3-70B-Instruct" {
			t.Errorf("ID = %q, want %q", info.ID, "meta-llama/Llama-3.3-70B-Instruct")
		}
		if info.DisplayName != "Llama 3.3 70B Instruct" {
			t.Errorf("DisplayName = %q, want %q", info.DisplayName, "Llama 3.3 70B Instruct")
		}
	})

	t.Run("existing model case insensitive", func(t *testing.T) {
		info, err := client.ModelInfo(ctx, "META-LLAMA/LLAMA-3.3-70B-INSTRUCT")
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
		info1, _ := client.ModelInfo(ctx, "meta-llama/Llama-3.3-70B-Instruct")
		info2, _ := client.ModelInfo(ctx, "meta-llama/Llama-3.3-70B-Instruct")

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

// TestConcurrentListModels tests thread-safety of ListModels.
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

// TestConcurrentModelInfo tests thread-safety of ModelInfo.
func TestConcurrentModelInfo(t *testing.T) {
	client := &Client{}
	ctx := context.Background()
	const goroutines = 100
	done := make(chan bool, goroutines)

	modelIDs := []string{
		"meta-llama/Llama-3.3-70B-Instruct",
		"Qwen/Qwen2.5-72B-Instruct",
		"mistralai/Mixtral-8x7B-Instruct-v0.1",
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

// TestModelInfoReturnsCopy verifies that ModelInfo returns a copy.
func TestModelInfoReturnsCopy(t *testing.T) {
	client := &Client{}
	ctx := context.Background()

	info1, _ := client.ModelInfo(ctx, "meta-llama/Llama-3.3-70B-Instruct")
	info2, _ := client.ModelInfo(ctx, "meta-llama/Llama-3.3-70B-Instruct")

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

// TestPaginationEdgeCases tests various pagination edge cases.
func TestPaginationEdgeCases(t *testing.T) {
	client := &Client{}
	ctx := context.Background()

	t.Run("empty cursor returns from start", func(t *testing.T) {
		result, err := client.ListModels(ctx, llms.WithModelsCursor(""), llms.WithModelsLimit(3))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(result.Models) != 3 {
			t.Errorf("expected 3 models, got %d", len(result.Models))
		}
		// First model should be the first cached model
		if result.Models[0].ID != cachedModels[0].ID {
			t.Errorf("expected first model to be %s, got %s", cachedModels[0].ID, result.Models[0].ID)
		}
	})

	t.Run("unknown cursor returns from start", func(t *testing.T) {
		// If cursor not found, pagination starts from beginning
		result, err := client.ListModels(ctx, llms.WithModelsCursor("unknown/cursor"))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		// Should return all models since cursor wasn't found
		if len(result.Models) != len(cachedModels) {
			t.Errorf("expected %d models, got %d", len(cachedModels), len(result.Models))
		}
	})

	t.Run("limit larger than total returns all", func(t *testing.T) {
		result, err := client.ListModels(ctx, llms.WithModelsLimit(1000))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(result.Models) != len(cachedModels) {
			t.Errorf("expected %d models, got %d", len(cachedModels), len(result.Models))
		}
		if result.HasMore {
			t.Error("HasMore should be false when all models returned")
		}
	})

	t.Run("limit of 0 returns all", func(t *testing.T) {
		result, err := client.ListModels(ctx, llms.WithModelsLimit(0))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(result.Models) != len(cachedModels) {
			t.Errorf("expected %d models, got %d", len(cachedModels), len(result.Models))
		}
		if result.HasMore {
			t.Error("HasMore should be false when limit is 0")
		}
	})

	t.Run("cursor after last model returns empty", func(t *testing.T) {
		lastModelID := cachedModels[len(cachedModels)-1].ID
		result, err := client.ListModels(ctx, llms.WithModelsCursor(lastModelID))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(result.Models) != 0 {
			t.Errorf("expected 0 models after last cursor, got %d", len(result.Models))
		}
		if result.HasMore {
			t.Error("HasMore should be false after last model")
		}
		if result.NextCursor != "" {
			t.Errorf("NextCursor should be empty, got %s", result.NextCursor)
		}
	})

	t.Run("combined cursor and type filter", func(t *testing.T) {
		// Get all code models first
		allCode, err := client.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeCode))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(allCode.Models) < 2 {
			t.Skip("need at least 2 code models for this test")
		}

		// Get second page with cursor and type filter
		firstCodeModel := allCode.Models[0].ID
		result, err := client.ListModels(ctx,
			llms.WithModelTypes(llms.ModelTypeCode),
			llms.WithModelsCursor(firstCodeModel),
		)
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}

		// Should not include the first model
		for _, m := range result.Models {
			if m.ID == firstCodeModel {
				t.Errorf("cursor model %s should not be in results", firstCodeModel)
			}
		}

		// All returned models should have code type
		for _, m := range result.Models {
			hasCode := false
			for _, typ := range m.Types {
				if typ == llms.ModelTypeCode {
					hasCode = true
					break
				}
			}
			if !hasCode {
				t.Errorf("model %s should have code type", m.ID)
			}
		}
	})

	t.Run("full pagination traversal", func(t *testing.T) {
		var allModels []llms.ModelInfo
		cursor := ""
		pageCount := 0

		for {
			var opts []llms.ListModelsOption
			opts = append(opts, llms.WithModelsLimit(5))
			if cursor != "" {
				opts = append(opts, llms.WithModelsCursor(cursor))
			}

			result, err := client.ListModels(ctx, opts...)
			if err != nil {
				t.Fatalf("ListModels() error at page %d: %v", pageCount, err)
			}

			allModels = append(allModels, result.Models...)
			pageCount++

			if !result.HasMore {
				break
			}
			cursor = result.NextCursor

			// Safety limit
			if pageCount > 100 {
				t.Fatal("too many pages, likely infinite loop")
			}
		}

		if len(allModels) != len(cachedModels) {
			t.Errorf("full pagination got %d models, expected %d", len(allModels), len(cachedModels))
		}

		// Check for duplicates
		seen := make(map[string]bool)
		for _, m := range allModels {
			if seen[m.ID] {
				t.Errorf("duplicate model %s in pagination results", m.ID)
			}
			seen[m.ID] = true
		}
	})
}
