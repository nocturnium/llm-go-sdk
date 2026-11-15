package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// catalogPageSize is the per-page size requested from /models/search.
const catalogPageSize = 100

// maxCatalogPages bounds the paging loop. The catalog held 75 models on
// 2026-11-14; result_info.total_count reported 334, so it is not trusted.
const maxCatalogPages = 20

type catalogModel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	Task        struct {
		Name string `json:"name"`
	} `json:"task"`
	Properties []struct {
		ID    string          `json:"property_id"`
		Value json.RawMessage `json:"value"`
	} `json:"properties"`
}

// ListModels reads the account catalog from /ai/models/search, paging until a
// short page. Types come from the catalog task, with decision models (which the
// catalog files under Text Generation) typed ModelTypeDecision. ContextLength and
// the vision flag come from catalog properties. Limit and type filters are
// applied locally; cursors return ErrInvalidParameters. Each call returns
// independent data.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	o := llms.ApplyListModelsOptions(opts...)
	if o.Cursor != "" || o.Limit < 0 {
		return nil, fmt.Errorf("cloudflare: unsupported cursor or negative limit: %w", llms.ErrInvalidParameters)
	}
	var models []llms.ModelInfo
	for page := 1; page <= maxCatalogPages; page++ {
		query := url.Values{"per_page": {strconv.Itoa(catalogPageSize)}, "page": {strconv.Itoa(page)}}
		env, err := c.send(ctx, http.MethodGet, []string{"models", "search"}, query, nil, false)
		if err != nil {
			return nil, err
		}
		var batch []catalogModel
		if err := json.Unmarshal(env.Result, &batch); err != nil {
			return nil, fmt.Errorf("cloudflare: decode model catalog: %w: %w", llms.ErrIncompleteResponse, err)
		}
		pageModels := make([]llms.ModelInfo, 0, len(batch))
		for _, m := range batch {
			pageModels = append(pageModels, c.modelInfo(m))
		}
		if len(o.Types) > 0 {
			pageModels = llms.FilterModelsByType(pageModels, o.Types...)
		}
		models = append(models, pageModels...)
		// The last page is the first one shorter than the page size the server
		// says it used (it may cap per_page below the request). A Limit is
		// satisfied once enough matching models are in hand.
		pageSize := env.ResultInfo.PerPage
		if pageSize <= 0 || pageSize > catalogPageSize {
			pageSize = catalogPageSize
		}
		if len(batch) == 0 || len(batch) < pageSize || (o.Limit > 0 && len(models) >= o.Limit) {
			break
		}
	}
	if models == nil {
		models = []llms.ModelInfo{}
	}
	if o.Limit > 0 && len(models) > o.Limit {
		models = models[:o.Limit]
	}
	return &llms.ListModelsResult{Models: models}, nil
}

// ModelInfo matches id case-insensitively against a fresh catalog read and
// returns a wrapped ErrModelNotFound for an unknown ID.
func (c *Client) ModelInfo(ctx context.Context, id string) (*llms.ModelInfo, error) {
	result, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range result.Models {
		if strings.EqualFold(m.ID, id) {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("cloudflare: model %q: %w", id, llms.ErrModelNotFound)
}

func (c *Client) modelInfo(m catalogModel) llms.ModelInfo {
	info := llms.ModelInfo{ID: m.Name, DisplayName: m.Name, Provider: c.Provider(), Description: m.Description, Types: []llms.ModelType{}}
	if _, ok := decisionModels[m.Name]; ok {
		info.Types = append(info.Types, llms.ModelTypeDecision)
	} else if t, ok := taskTypes[m.Task.Name]; ok {
		info.Types = append(info.Types, t)
	}
	if m.Task.Name == "Text Classification" && strings.Contains(m.Name, "reranker") {
		info.Types = append(info.Types, llms.ModelTypeRerank)
	}
	for _, p := range m.Properties {
		switch p.ID {
		case "context_window":
			info.ContextLength = propertyInt(p.Value)
		case "vision":
			if propertyString(p.Value) == "true" {
				info.Types = append(info.Types, llms.ModelTypeVision)
			}
		}
	}
	// Live format "2026-09-29 09:25:44.258". time.Parse accepts a fractional
	// second after the seconds field even when the layout omits it;
	// TestModelInfo_CreatedAt pins that against the live value.
	if t, err := time.Parse("2006-01-02 15:04:05", m.CreatedAt); err == nil {
		info.CreatedAt = t
	}
	return info
}

// taskTypes maps Workers AI catalog tasks to model types. Tasks without an SDK
// interface (Translation, Image Classification, Voice Activity Detection) map
// to nothing.
var taskTypes = map[string]llms.ModelType{
	"Text Generation":              llms.ModelTypeChat,
	"Text Embeddings":              llms.ModelTypeEmbedding,
	"Multimodal Embeddings":        llms.ModelTypeEmbedding,
	"Text-to-Image":                llms.ModelTypeImage,
	"Text-to-Speech":               llms.ModelTypeAudio,
	"Automatic Speech Recognition": llms.ModelTypeAudio,
	"Image-to-Text":                llms.ModelTypeVision,
}

// propertyString reads a catalog property sent as a JSON string.
func propertyString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// propertyInt reads a catalog property sent as a JSON string or number.
func propertyInt(raw json.RawMessage) int {
	if n, err := strconv.Atoi(propertyString(raw)); err == nil {
		return n
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	return 0
}
