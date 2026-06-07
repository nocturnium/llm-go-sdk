package llms

import (
	"sort"
	"strings"
)

// FilterModelsByType returns models that support any of the specified types.
// If types is empty, returns a copy of all models.
// Always returns a new slice to prevent mutation of the original.
func FilterModelsByType(models []ModelInfo, types ...ModelType) []ModelInfo {
	if len(models) == 0 {
		return []ModelInfo{}
	}

	if len(types) == 0 {
		// Return a copy to prevent mutation of original
		result := make([]ModelInfo, len(models))
		copy(result, models)
		return result
	}

	// Use a set for O(1) type lookups
	typeSet := make(map[ModelType]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}

	result := make([]ModelInfo, 0, len(models))
	for _, m := range models {
		for _, mt := range m.Types {
			if _, ok := typeSet[mt]; ok {
				result = append(result, m)
				break
			}
		}
	}
	return result
}

// FilterChatModels returns models that support chat/conversation.
func FilterChatModels(models []ModelInfo) []ModelInfo {
	return FilterModelsByType(models, ModelTypeChat)
}

// FilterEmbeddingModels returns models that generate embeddings.
func FilterEmbeddingModels(models []ModelInfo) []ModelInfo {
	return FilterModelsByType(models, ModelTypeEmbedding)
}

// FilterVisionModels returns models that can process images as input.
func FilterVisionModels(models []ModelInfo) []ModelInfo {
	return FilterModelsByType(models, ModelTypeVision)
}

// FilterCodeModels returns models optimized for code generation.
func FilterCodeModels(models []ModelInfo) []ModelInfo {
	return FilterModelsByType(models, ModelTypeCode)
}

// FilterModelsByProvider returns models from a specific provider.
func FilterModelsByProvider(models []ModelInfo, provider Provider) []ModelInfo {
	result := make([]ModelInfo, 0, len(models))
	for _, m := range models {
		if m.Provider == provider {
			result = append(result, m)
		}
	}
	return result
}

// FilterModelsByOrganization returns models from a specific organization.
// The match is case-insensitive.
func FilterModelsByOrganization(models []ModelInfo, org string) []ModelInfo {
	orgLower := strings.ToLower(org)
	result := make([]ModelInfo, 0)
	for _, m := range models {
		if strings.ToLower(m.Organization) == orgLower {
			result = append(result, m)
		}
	}
	return result
}

// FilterModelsByMinContext returns models with at least the specified context length.
func FilterModelsByMinContext(models []ModelInfo, minTokens int) []ModelInfo {
	result := make([]ModelInfo, 0)
	for _, m := range models {
		if m.ContextLength >= minTokens {
			result = append(result, m)
		}
	}
	return result
}

// SearchModels returns models where the ID or DisplayName contains the query.
// The search is case-insensitive.
func SearchModels(models []ModelInfo, query string) []ModelInfo {
	queryLower := strings.ToLower(query)
	result := make([]ModelInfo, 0)
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.ID), queryLower) ||
			strings.Contains(strings.ToLower(m.DisplayName), queryLower) {
			result = append(result, m)
		}
	}
	return result
}

// SortModelsByContextLength sorts models by context window size.
// Use descending=true for largest first, false for smallest first.
func SortModelsByContextLength(models []ModelInfo, descending bool) []ModelInfo {
	result := make([]ModelInfo, len(models))
	copy(result, models)

	sort.Slice(result, func(i, j int) bool {
		if descending {
			return result[i].ContextLength > result[j].ContextLength
		}
		return result[i].ContextLength < result[j].ContextLength
	})
	return result
}

// SortModelsByInputPrice sorts models by input token price.
// Models without pricing are sorted to the end.
// Use descending=true for most expensive first.
func SortModelsByInputPrice(models []ModelInfo, descending bool) []ModelInfo {
	result := make([]ModelInfo, len(models))
	copy(result, models)

	sort.Slice(result, func(i, j int) bool {
		priceI := 0.0
		priceJ := 0.0
		hasI := result[i].Pricing != nil
		hasJ := result[j].Pricing != nil

		if hasI {
			priceI = result[i].Pricing.Input
		}
		if hasJ {
			priceJ = result[j].Pricing.Input
		}

		// Models without pricing go to the end
		if !hasI && hasJ {
			return false
		}
		if hasI && !hasJ {
			return true
		}

		if descending {
			return priceI > priceJ
		}
		return priceI < priceJ
	})
	return result
}

// SortModelsByName sorts models alphabetically by ID.
func SortModelsByName(models []ModelInfo, descending bool) []ModelInfo {
	result := make([]ModelInfo, len(models))
	copy(result, models)

	sort.Slice(result, func(i, j int) bool {
		if descending {
			return result[i].ID > result[j].ID
		}
		return result[i].ID < result[j].ID
	})
	return result
}

// SortModelsByCreatedAt sorts models by creation date.
// Use descending=true for newest first.
func SortModelsByCreatedAt(models []ModelInfo, descending bool) []ModelInfo {
	result := make([]ModelInfo, len(models))
	copy(result, models)

	sort.Slice(result, func(i, j int) bool {
		if descending {
			return result[i].CreatedAt.After(result[j].CreatedAt)
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

// FindModelByID returns a copy of the first model with the given ID, or nil if not found.
// Returns a copy to prevent aliasing issues.
func FindModelByID(models []ModelInfo, id string) *ModelInfo {
	for _, m := range models {
		if m.ID == id {
			result := m // Copy the value
			return &result
		}
	}
	return nil
}

// GroupModelsByProvider groups models by their provider.
func GroupModelsByProvider(models []ModelInfo) map[Provider][]ModelInfo {
	result := make(map[Provider][]ModelInfo)
	for _, m := range models {
		result[m.Provider] = append(result[m.Provider], m)
	}
	return result
}

// GroupModelsByType groups models by their types.
// A model may appear in multiple groups if it has multiple types.
func GroupModelsByType(models []ModelInfo) map[ModelType][]ModelInfo {
	result := make(map[ModelType][]ModelInfo)
	for _, m := range models {
		for _, t := range m.Types {
			result[t] = append(result[t], m)
		}
	}
	return result
}

// GetUniqueOrganizations returns a list of unique organizations from the models.
func GetUniqueOrganizations(models []ModelInfo) []string {
	seen := make(map[string]bool)
	var result []string
	for _, m := range models {
		if m.Organization != "" && !seen[m.Organization] {
			seen[m.Organization] = true
			result = append(result, m.Organization)
		}
	}
	sort.Strings(result)
	return result
}

// GetUniqueTypes returns a list of unique model types from the models.
func GetUniqueTypes(models []ModelInfo) []ModelType {
	seen := make(map[ModelType]bool)
	var result []ModelType
	for _, m := range models {
		for _, t := range m.Types {
			if !seen[t] {
				seen[t] = true
				result = append(result, t)
			}
		}
	}
	return result
}

// ApplyModelFilters applies ListModelsOptions filters to a slice of models.
// This is useful for providers using cached data.
// Always returns a copy to prevent mutation of the original slice.
func ApplyModelFilters(models []ModelInfo, opts *ListModelsOptions) []ModelInfo {
	if len(models) == 0 {
		return []ModelInfo{}
	}

	// Start with a copy - FilterModelsByType always returns a new slice
	var result []ModelInfo
	if opts != nil && len(opts.Types) > 0 {
		result = FilterModelsByType(models, opts.Types...)
	} else {
		// No type filter, make a copy
		result = make([]ModelInfo, len(models))
		copy(result, models)
	}

	// Apply limit if specified
	if opts != nil && opts.Limit > 0 && len(result) > opts.Limit {
		result = result[:opts.Limit]
	}

	return result
}
