package llms

import (
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

func TestWebSearchConfig_Defaults(t *testing.T) {
	config := WebSearchConfig{
		Enabled: true,
	}

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

func TestWebSearchConfig_Full(t *testing.T) {
	config := WebSearchConfig{
		Enabled:        true,
		Provider:       WebSearchBrave,
		APIKey:         "test-api-key",
		ResultCount:    5,
		DomainFilter:   []string{"example.com", "test.org"},
		DomainExclude:  []string{"spam.com"},
		RecencyFilter:  "week",
		IncludeResults: true,
	}

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

func TestSearchResult_Basic(t *testing.T) {
	now := time.Now()
	result := SearchResult{
		Title:   "Example Page",
		URL:     "https://example.com/page",
		Snippet: "This is an example snippet...",
		Date:    &now,
	}

	if result.Title != "Example Page" {
		t.Errorf("expected title, got %s", result.Title)
	}
	if result.URL != "https://example.com/page" {
		t.Errorf("expected URL, got %s", result.URL)
	}
	if result.Date == nil {
		t.Error("expected date")
	}
}
