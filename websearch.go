package llms

import "time"

// WebSearchProvider specifies which search provider to use
type WebSearchProvider string

const (
	// WebSearchAuto uses the provider's native search if available,
	// otherwise falls back to the configured default external provider
	WebSearchAuto WebSearchProvider = "auto"

	// WebSearchNative forces the provider's native search (fails if unavailable)
	WebSearchNative WebSearchProvider = "native"

	// External search providers
	WebSearchBrave  WebSearchProvider = "brave"
	WebSearchTavily WebSearchProvider = "tavily"
)

// WebSearchConfig configures web search for providers that support it.
// Providers may implement this natively (Z.AI, Perplexity, Gemini) or via
// external tools (Brave Search, Tavily).
type WebSearchConfig struct {
	// Enabled turns web search on/off
	Enabled bool `json:"enabled"`

	// Provider specifies which search provider to use.
	// Default (empty) is equivalent to WebSearchAuto.
	Provider WebSearchProvider `json:"provider"`

	// APIKey is the per-call key for external search providers.
	// Native provider search generally ignores this field.
	APIKey string `json:"api_key"`

	// ResultCount is the number of search results to retrieve (1-50).
	// Zero uses provider default (typically 10).
	ResultCount int `json:"result_count"`

	// DomainFilter limits search to specific domains.
	// Empty means no restriction.
	DomainFilter []string `json:"domain_filter"`

	// DomainExclude excludes specific domains from results.
	DomainExclude []string `json:"domain_exclude"`

	// RecencyFilter limits results by age: "day", "week", "month", "year".
	// Empty means no time restriction.
	RecencyFilter string `json:"recency_filter"`

	// IncludeResults includes raw search results in response metadata.
	// Useful for citations and source attribution.
	IncludeResults bool `json:"include_results"`
}

// SearchResult represents a single web search result.
type SearchResult struct {
	// Title is the page title
	Title string `json:"title"`

	// URL is the page URL
	URL string `json:"url"`

	// Snippet is a text excerpt from the page
	Snippet string `json:"snippet"`

	// Date is the publication date if available
	Date *time.Time `json:"date,omitempty"`
}
