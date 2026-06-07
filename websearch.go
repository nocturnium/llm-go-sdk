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
	Enabled bool

	// Provider specifies which search provider to use.
	// Default (empty) is equivalent to WebSearchAuto.
	Provider WebSearchProvider

	// APIKey is the per-call key for external search providers.
	// Native provider search generally ignores this field.
	APIKey string

	// ResultCount is the number of search results to retrieve (1-50).
	// Zero uses provider default (typically 10).
	ResultCount int

	// DomainFilter limits search to specific domains.
	// Empty means no restriction.
	DomainFilter []string

	// DomainExclude excludes specific domains from results.
	DomainExclude []string

	// RecencyFilter limits results by age: "day", "week", "month", "year".
	// Empty means no time restriction.
	RecencyFilter string

	// IncludeResults includes raw search results in response metadata.
	// Useful for citations and source attribution.
	IncludeResults bool
}

// SearchResult represents a single web search result.
type SearchResult struct {
	// Title is the page title
	Title string

	// URL is the page URL
	URL string

	// Snippet is a text excerpt from the page
	Snippet string

	// Date is the publication date if available
	Date *time.Time
}
