package llms

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWebSearchProvider_Constants(t *testing.T) {
	providers := []WebSearchProvider{
		WebSearchAuto,
		WebSearchNative,
		WebSearchBrave,
		WebSearchTavily,
	}

	expected := []string{"auto", "native", "brave", "tavily"}

	for i, p := range providers {
		if string(p) != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], p)
		}
	}
}

// WithWebSearchEnabled is the documented one-liner, so what it builds is what
// this pins rather than a struct literal read back.
func TestWebSearchConfig_Defaults(t *testing.T) {
	config := *ApplyOptions(WithWebSearchEnabled()).WebSearch

	if !config.Enabled {
		t.Error("expected enabled")
	}
	if config.Provider != "" {
		t.Errorf("expected empty provider (defaults to auto), got %s", config.Provider)
	}
	if config.ResultCount != 0 {
		t.Errorf("expected 0 result count (use provider default), got %d", config.ResultCount)
	}
	if config.APIKey != "" {
		t.Error("expected empty API key")
	}
}

// A fully specified config reaches the call options the providers read, which is
// the only path it travels.
func TestWebSearchConfig_Full(t *testing.T) {
	config := *ApplyOptions(WithWebSearch(WebSearchConfig{
		Enabled:        true,
		Provider:       WebSearchBrave,
		APIKey:         "test-api-key",
		ResultCount:    5,
		DomainFilter:   []string{"example.com", "test.org"},
		DomainExclude:  []string{"spam.com"},
		RecencyFilter:  "week",
		IncludeResults: true,
	})).WebSearch

	if config.Provider != WebSearchBrave {
		t.Errorf("expected brave, got %s", config.Provider)
	}
	if config.APIKey != "test-api-key" {
		t.Errorf("expected per-call API key, got %q", config.APIKey)
	}
	if len(config.DomainFilter) != 2 {
		t.Errorf("expected 2 domain filters, got %d", len(config.DomainFilter))
	}
	if !config.IncludeResults {
		t.Error("expected IncludeResults true")
	}
}

// Search results reach a caller on the Response, and a nil-safe read of an
// absent one is what the accessor promises.
func TestSearchResult_Basic(t *testing.T) {
	now := time.Now()
	resp := &Response{SearchResults: []SearchResult{{
		Title:   "Example Page",
		URL:     "https://example.com/page",
		Snippet: "This is an example snippet...",
		Date:    &now,
	}}}

	if len(resp.SearchResults) != 1 || resp.SearchResults[0].URL != "https://example.com/page" {
		t.Fatalf("search results = %+v", resp.SearchResults)
	}
	var empty *Response
	if empty.ReasoningText() != "" {
		t.Error("a nil Response is not readable")
	}
}

func TestSearchResult_JSONTags(t *testing.T) {
	result := SearchResult{
		Title:   "Example Page",
		URL:     "https://example.com/page",
		Snippet: "This is an example snippet...",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{"title":"Example Page","url":"https://example.com/page","snippet":"This is an example snippet..."}`
	if string(data) != want {
		t.Errorf("json = %s, want %s", data, want)
	}
}
