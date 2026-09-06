package elevenlabs

import (
	"context"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Catalog verified 2026-09-05. Context token counts are unknown; character limits
// are validated by speech requests instead of being mislabeled as token limits.
var knownModels = buildKnownModels()

func buildKnownModels() []llms.ModelInfo {
	groups := []struct {
		typ llms.ModelType
		ids []string
	}{
		{llms.ModelTypeAudio, []string{"eleven_v3", "eleven_v3_conversational", "eleven_multilingual_v2", "eleven_flash_v2_5", "eleven_turbo_v2_5", "eleven_turbo_v2", "eleven_flash_v2", "scribe_v2", "eleven_text_to_sound_v2", "music_v2", "music_v1"}},
		{llms.ModelTypeImage, []string{"gemini-3.1-flash-lite-image", "gemini-3.1-flash-image", "gemini-3-pro-image", "gpt-image-1", "gpt-image-1.5", "gpt-image-2", "bytedance-seedream-5-lite", "bytedance-seedream-5-pro"}},
		{llms.ModelTypeVideo, []string{"veo-3.1-fast-generate-001", "veo-3.1-generate-001", "bytedance-seedance-v2", "bytedance-seedance-v2-fast", "bytedance-seedance-v2-mini", "bytedance-seedance-v2.5"}},
	}
	var models []llms.ModelInfo
	for _, group := range groups {
		for _, id := range group.ids {
			organization := "ElevenLabs"
			switch {
			case strings.HasPrefix(id, "veo-"):
				organization = "Google"
			case strings.HasPrefix(id, "gpt-image-"):
				organization = "OpenAI"
			case strings.HasPrefix(id, "bytedance-"):
				organization = "ByteDance"
			}
			models = append(models, llms.ModelInfo{ID: id, DisplayName: id, Provider: llms.ProviderElevenLabs, Organization: organization, Types: []llms.ModelType{group.typ}, FromCache: true})
		}
	}
	return models
}
func copyModel(info llms.ModelInfo) llms.ModelInfo {
	info.Types = append([]llms.ModelType(nil), info.Types...)
	return info
}

// ListModels returns independent copies of the static media catalog. Types use
// OR filtering; cursors are the last model ID from a previous page. Invalid
// cursors/limits return ErrInvalidParameters. Dedicated filtering is ignored.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, WrapError("list models", err)
	}
	o := llms.ApplyListModelsOptions(opts...)
	if o.Limit < 0 {
		return nil, invalid("negative model limit")
	}
	filtered := []llms.ModelInfo{}
	for _, info := range knownModels {
		match := len(o.Types) == 0
		for _, typ := range o.Types {
			if info.HasType(typ) {
				match = true
			}
		}
		if match {
			filtered = append(filtered, copyModel(info))
		}
	}
	start := 0
	if o.Cursor != "" {
		found := false
		for i, info := range filtered {
			if info.ID == o.Cursor {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, invalid("unknown model cursor")
		}
	}
	end := len(filtered)
	if o.Limit > 0 && o.Limit < end-start {
		end = start + o.Limit
	}
	result := &llms.ListModelsResult{Models: filtered[start:end], HasMore: end < len(filtered)}
	if result.HasMore {
		result.NextCursor = filtered[end-1].ID
	}
	return result, nil
}

// ModelInfo returns a copy of cached metadata by case-insensitive ID, or
// ErrModelNotFound for an unknown ID; cancellation is checked before lookup.
func (c *Client) ModelInfo(ctx context.Context, id string) (*llms.ModelInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, WrapError("model info", err)
	}
	for _, info := range knownModels {
		if strings.EqualFold(info.ID, id) {
			out := copyModel(info)
			return &out, nil
		}
	}
	return nil, WrapError("model info", llms.ErrModelNotFound)
}
