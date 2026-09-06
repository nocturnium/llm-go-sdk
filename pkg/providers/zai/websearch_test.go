package zai

import (
	"encoding/json"
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestBuildWebSearchTool_Disabled(t *testing.T) {
	cfg := &llms.WebSearchConfig{Enabled: false}
	tool := BuildWebSearchTool(cfg)
	if tool != nil {
		t.Error("expected nil for disabled web search")
	}
}

func TestBuildWebSearchTool_Nil(t *testing.T) {
	tool := BuildWebSearchTool(nil)
	if tool != nil {
		t.Error("expected nil for nil config")
	}
}

func TestBuildWebSearchTool_Enabled(t *testing.T) {
	cfg := &llms.WebSearchConfig{
		Enabled: true,
	}
	tool := BuildWebSearchTool(cfg)

	if tool == nil {
		t.Fatal("expected tool")
	}
	if tool.Type != "web_search" {
		t.Errorf("expected type web_search, got %s", tool.Type)
	}
	if tool.WebSearch.Enable != "True" {
		t.Errorf("expected enable True, got %s", tool.WebSearch.Enable)
	}
}

func TestBuildWebSearchTool_FullConfig(t *testing.T) {
	cfg := &llms.WebSearchConfig{
		Enabled:       true,
		ResultCount:   5,
		DomainFilter:  []string{"example.com", "test.org"},
		RecencyFilter: "week",
	}
	tool := BuildWebSearchTool(cfg)

	if tool.WebSearch.Count != "5" {
		t.Errorf("expected count 5, got %s", tool.WebSearch.Count)
	}
	if tool.WebSearch.SearchDomainFilter != "example.com,test.org" {
		t.Errorf("expected domains, got %s", tool.WebSearch.SearchDomainFilter)
	}
	if tool.WebSearch.SearchRecencyFilter != "week" {
		t.Errorf("expected week, got %s", tool.WebSearch.SearchRecencyFilter)
	}
}

func TestBuildWebSearchTool_JSON(t *testing.T) {
	cfg := &llms.WebSearchConfig{
		Enabled:        true,
		ResultCount:    10,
		IncludeResults: true,
	}
	tool := BuildWebSearchTool(cfg)

	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	expected := `{"type":"web_search","web_search":{"enable":"True","search_result":"True","count":"10"}}`
	if string(data) != expected {
		t.Errorf("unexpected JSON:\ngot:  %s\nwant: %s", string(data), expected)
	}
}

func TestBuildWebSearchTool_IncludeResultsOptIn(t *testing.T) {
	tool := BuildWebSearchTool(&llms.WebSearchConfig{Enabled: true})
	if tool.WebSearch.SearchResult != "" {
		t.Fatalf("search_result must be omitted unless IncludeResults is set, got %q", tool.WebSearch.SearchResult)
	}
	data, _ := json.Marshal(tool)
	if string(data) != `{"type":"web_search","web_search":{"enable":"True"}}` {
		t.Fatalf("json = %s", data)
	}
}

func TestNewWebSearchTool_DomainExclude(t *testing.T) {
	_, err := NewWebSearchTool(&llms.WebSearchConfig{Enabled: true, DomainExclude: []string{"spam.example"}})
	if !errors.Is(err, ErrWebSearchDomainExclude) || !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("err = %v", err)
	}
	if tool, err := NewWebSearchTool(&llms.WebSearchConfig{Enabled: false, DomainExclude: []string{"spam.example"}}); tool != nil || err != nil {
		t.Fatalf("disabled config must not be validated: %v %v", tool, err)
	}
	if tool, err := NewWebSearchTool(&llms.WebSearchConfig{Enabled: true, DomainFilter: []string{"a.example"}}); err != nil || tool.WebSearch.SearchDomainFilter != "a.example" {
		t.Fatalf("tool = %+v err = %v", tool, err)
	}
	if BuildWebSearchTool(&llms.WebSearchConfig{Enabled: true, DomainExclude: []string{"x"}}) == nil {
		t.Fatal("lenient builder must still return a tool")
	}
}
