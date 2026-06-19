package llms

// This file provides a small, dependency-free way to compose middleware around a
// base LLM. v3 moved the observability and resilience middleware out of the root
// package into pkg/observability and pkg/middleware/resilience, so wiring the full
// kit means several constructors. Chain keeps that composition flat and readable
// without re-exporting those packages (a root "facade" would create import cycles;
// see docs/ARCHITECTURE.md).

// Middleware wraps an LLM to add behavior — resilience, observability, caching,
// rate limiting, and so on. It takes the next LLM in the chain and returns an LLM
// that wraps it. The subpackage constructors (e.g. resilience.NewResilientClient,
// observability.NewOTelMiddleware, CachedClient) adapt to this shape with a one-line
// closure.
type Middleware = func(LLM) LLM

// Chain composes middleware around a base LLM. Middleware are applied in order, so
// the first listed sits closest to the base provider (innermost) and the last
// listed is outermost. The recommended order puts resilience and fallback innermost
// — so retries and failover happen before metrics and cost are recorded once for
// the whole call — and observability and logging outermost. nil entries are
// skipped; Chain returns base unchanged when no middleware is given.
//
//	client := llms.Chain(base,
//	    func(next llms.LLM) llms.LLM { return resilience.NewResilientClient(next, resilience.DefaultRetryConfig()) },
//	    func(next llms.LLM) llms.LLM { return observability.NewOTelMiddleware(next) },
//	)
//	// observability(resilience(base)) — resilience innermost, observability outermost.
func Chain(base LLM, middleware ...Middleware) LLM {
	for _, mw := range middleware {
		if mw != nil {
			base = mw(base)
		}
	}
	return base
}
