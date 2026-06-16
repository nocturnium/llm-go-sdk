package zai

import (
	"fmt"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v2"
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

// BuildWebSearchTool creates a Z.AI web search tool from WebSearchConfig.
// Returns nil if web search is not enabled.
func BuildWebSearchTool(cfg *llms.WebSearchConfig) *WebSearchTool {
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	params := &WebSearchParams{
		Enable:       "True",
		SearchResult: "True",
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
