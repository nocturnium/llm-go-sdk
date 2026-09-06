package zai

import (
	"fmt"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// WebSearchTool represents Z.AI's native web search tool format.
type WebSearchTool struct {
	Type      string           `json:"type"`
	WebSearch *WebSearchParams `json:"web_search"`
}

// WebSearchParams contains the web search configuration.
type WebSearchParams struct {
	Enable              string `json:"enable"`
	SearchResult        string `json:"search_result,omitempty"`
	Count               string `json:"count,omitempty"`
	SearchDomainFilter  string `json:"search_domain_filter,omitempty"`
	SearchRecencyFilter string `json:"search_recency_filter,omitempty"`
}

// ErrWebSearchDomainExclude reports a WebSearchConfig.DomainExclude that Z.AI
// cannot honor: its web_search tool only supports an allow-list
// (search_domain_filter).
var ErrWebSearchDomainExclude = fmt.Errorf("zai: web search does not support DomainExclude; use DomainFilter: %w", llms.ErrInvalidParameters)

// NewWebSearchTool creates a Z.AI web search tool from WebSearchConfig. It
// returns (nil, nil) when web search is not enabled and
// ErrWebSearchDomainExclude when DomainExclude is set, since Z.AI has no
// exclude-list parameter. Raw search results are requested only when
// cfg.IncludeResults is true.
func NewWebSearchTool(cfg *llms.WebSearchConfig) (*WebSearchTool, error) {
	if cfg != nil && cfg.Enabled && len(cfg.DomainExclude) > 0 {
		return nil, ErrWebSearchDomainExclude
	}
	return BuildWebSearchTool(cfg), nil
}

// BuildWebSearchTool creates a Z.AI web search tool from WebSearchConfig.
// Returns nil if web search is not enabled. It is the lenient form of
// NewWebSearchTool: an unsupported DomainExclude is ignored rather than
// rejected. Raw search results are requested only when cfg.IncludeResults is
// true.
func BuildWebSearchTool(cfg *llms.WebSearchConfig) *WebSearchTool {
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	params := &WebSearchParams{
		Enable: "True",
	}
	if cfg.IncludeResults {
		params.SearchResult = "True"
	}

	if cfg.ResultCount > 0 {
		params.Count = fmt.Sprintf("%d", cfg.ResultCount)
	}

	if len(cfg.DomainFilter) > 0 {
		// Z.AI expects comma-separated domains
		params.SearchDomainFilter = strings.Join(cfg.DomainFilter, ",")
	}

	if cfg.RecencyFilter != "" {
		params.SearchRecencyFilter = cfg.RecencyFilter
	}

	return &WebSearchTool{
		Type:      "web_search",
		WebSearch: params,
	}
}
