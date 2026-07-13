package resilience

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

// Fallback-related errors
var (
	ErrNoClientsAvailable = errors.New("no LLM clients available in fallback chain")
	ErrAllClientsFailed   = errors.New("all LLM clients in fallback chain failed")
)

const defaultFallbackRecoveryAfter = 30 * time.Second

// FallbackSelector determines whether to try the next client in the chain
type FallbackSelector interface {
	// ShouldFallback returns true if we should try the next client given the error
	ShouldFallback(err error) bool
}

// DefaultFallbackSelector falls back on rate limits, quota exceeded, and server errors
type DefaultFallbackSelector struct{}

// ShouldFallback implements FallbackSelector
func (s DefaultFallbackSelector) ShouldFallback(err error) bool {
	if err == nil {
		return false
	}

	if isProviderUnhealthy(err) {
		return true
	}

	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 529: // Overloaded (Anthropic)
			return true
		}

		// Check error type strings
		switch apiErr.Type {
		case "rate_limit_error", "overloaded_error", "server_error":
			return true
		}
	}

	// Check for circuit breaker
	if errors.Is(err, ErrCircuitOpen) {
		return true
	}

	return false
}

// AlwaysFallbackSelector falls back on any error
type AlwaysFallbackSelector struct{}

// ShouldFallback implements FallbackSelector
func (s AlwaysFallbackSelector) ShouldFallback(err error) bool {
	return err != nil
}

// NeverFallbackSelector never falls back (stops on first error)
type NeverFallbackSelector struct{}

// ShouldFallback implements FallbackSelector
func (s NeverFallbackSelector) ShouldFallback(_ error) bool {
	return false
}

// fallbackEntry pairs a client with a stable identity and its health state.
// The id is assigned once when the client is added and never reused, so health
// is bound to the client itself rather than to its shifting slice position —
// adding or removing a client mid-call can no longer misattribute a failure to
// a different client. unhealthyUntil is zero when healthy, else the instant the
// client becomes eligible for a half-open probe.
type fallbackEntry struct {
	id             int64
	client         llms.LLM
	unhealthyUntil time.Time
}

// FallbackChain tries multiple LLM clients in order until one succeeds
type FallbackChain struct {
	mu       sync.RWMutex
	entries  []fallbackEntry
	nextID   int64
	selector FallbackSelector

	// Callbacks
	onFallback func(fromIdx int, toIdx int, from, to llms.LLM, err error)
	onSuccess  func(idx int, client llms.LLM)

	// Health tracking is stored per-entry (keyed by stable id), not by slice
	// position, so a concurrent AddClient/RemoveClient during an in-flight call
	// cannot mark the wrong client unhealthy. Protected by mu.
	recoveryAfter time.Duration
	now           func() time.Time
}

// fallbackCandidate is an entry captured in a point-in-time snapshot for a
// single operation. pos is the client's position within that snapshot (used for
// the positional callback contract); id is its stable identity (used to record
// health back onto the right client regardless of later mutation).
type fallbackCandidate struct {
	pos    int
	id     int64
	client llms.LLM
}

// FallbackOption configures a FallbackChain
type FallbackOption func(*FallbackChain)

// NewFallbackChain creates a new fallback chain with the given clients
func NewFallbackChain(clients []llms.LLM, opts ...FallbackOption) *FallbackChain {
	fc := &FallbackChain{
		selector:      DefaultFallbackSelector{},
		entries:       make([]fallbackEntry, len(clients)),
		recoveryAfter: defaultFallbackRecoveryAfter,
		now:           time.Now,
	}
	for i, client := range clients {
		fc.entries[i] = fallbackEntry{id: fc.nextID, client: client}
		fc.nextID++
	}

	for _, opt := range opts {
		opt(fc)
	}

	return fc
}

// WithFallbackSelector sets a custom fallback selector
func WithFallbackSelector(selector FallbackSelector) FallbackOption {
	return func(fc *FallbackChain) {
		if selector != nil {
			fc.selector = selector
		}
	}
}

// WithOnFallback sets a callback that's called when falling back to the next client
func WithOnFallback(fn func(fromIdx int, toIdx int, from, to llms.LLM, err error)) FallbackOption {
	return func(fc *FallbackChain) {
		fc.onFallback = fn
	}
}

// WithOnSuccess sets a callback that's called when a client succeeds
func WithOnSuccess(fn func(idx int, client llms.LLM)) FallbackOption {
	return func(fc *FallbackChain) {
		fc.onSuccess = fn
	}
}

// WithRecoveryAfter sets how long a failed client remains in cooldown before it
// becomes eligible for a half-open probe.
func WithRecoveryAfter(d time.Duration) FallbackOption {
	return func(fc *FallbackChain) {
		if d > 0 {
			fc.recoveryAfter = d
		}
	}
}

func withFallbackClock(now func() time.Time) FallbackOption {
	return func(fc *FallbackChain) {
		if now != nil {
			fc.now = now
		}
	}
}

// Call tries each client in order until one succeeds
func (fc *FallbackChain) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	candidates := fc.snapshot()
	if len(candidates) == 0 {
		return "", ErrNoClientsAvailable
	}

	var lastErr error
	for pos, cand := range candidates {
		result, err := llms.Call(ctx, cand.client, prompt, options...)
		if err == nil {
			fc.markHealthyID(cand.id)
			if fc.onSuccess != nil {
				fc.onSuccess(cand.pos, cand.client)
			}
			return result, nil
		}

		lastErr = err
		if isProviderUnhealthy(err) {
			fc.markUnhealthyID(cand.id)
		}

		// Check if we should fallback
		if !fc.selector.ShouldFallback(err) {
			return "", err
		}

		// Callback for fallback
		fc.fireFallback(pos, candidates, err)
	}

	return "", allClientsFailedError(lastErr)
}

// executeWithFallback executes an operation across clients with fallback logic.
// The operation function receives a client and returns a result and error.
// This helper centralizes the retry/fallback logic used by GenerateContent and Stream.
func executeWithFallback[T any](fc *FallbackChain, operation func(client llms.LLM) (T, error)) (T, error) {
	candidates := fc.snapshot()
	var zero T
	if len(candidates) == 0 {
		return zero, ErrNoClientsAvailable
	}

	var lastErr error
	for pos, cand := range candidates {
		result, err := operation(cand.client)
		if err == nil {
			fc.markHealthyID(cand.id)
			if fc.onSuccess != nil {
				fc.onSuccess(cand.pos, cand.client)
			}
			return result, nil
		}

		lastErr = err
		if isProviderUnhealthy(err) {
			fc.markUnhealthyID(cand.id)
		}

		// Check if we should fallback
		if !fc.selector.ShouldFallback(err) {
			return zero, err
		}

		// Callback for fallback
		fc.fireFallback(pos, candidates, err)
	}

	return zero, allClientsFailedError(lastErr)
}

// GenerateContent tries each client in order until one succeeds
func (fc *FallbackChain) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	return executeWithFallback(fc, func(client llms.LLM) (*llms.Response, error) {
		return client.GenerateContent(ctx, messages, options...)
	})
}

// Stream tries each client in order until one succeeds
// Stream attempts each client in order. Because streaming failures surface as a
// terminal error chunk rather than the synchronous return value, it inspects the
// first chunk: if a client errors before emitting any content, it fails over to
// the next client; once content flows, that client is committed and streamed
// through (a later mid-stream error marks it unhealthy but cannot fail over).
func (fc *FallbackChain) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	candidates := fc.snapshot()
	if len(candidates) == 0 {
		return nil, ErrNoClientsAvailable
	}

	opts := llms.ApplyOptions(options...)
	var lastErr error

	for pos, cand := range candidates {
		client := cand.client
		src, err := client.Stream(ctx, messages, options...)
		if err != nil {
			lastErr = err
			if isProviderUnhealthy(err) {
				fc.markUnhealthyID(cand.id)
			}
			if !fc.selector.ShouldFallback(err) {
				return nil, err
			}
			fc.fireFallback(pos, candidates, err)
			continue
		}

		first, ok := <-src
		switch {
		case !ok:
			// Stream closed without any chunk; treat as an (empty) success.
			fc.markHealthyID(cand.id)
			return forwardStream(ctx, opts, llms.StreamChunk{Done: true}, src, nil), nil
		case first.Error != nil:
			lastErr = first.Error
			if isProviderUnhealthy(first.Error) {
				fc.markUnhealthyID(cand.id)
			}
			if !fc.selector.ShouldFallback(first.Error) {
				// Committed to this client by contract (chan already returned); deliver its error.
				return forwardStream(ctx, opts, first, src, nil), nil
			}
			fc.fireFallback(pos, candidates, first.Error)
			go drainStream(src) // let the abandoned producer finish without blocking
			continue
		default:
			fc.markHealthyID(cand.id)
			if fc.onSuccess != nil {
				fc.onSuccess(cand.pos, client)
			}
			id := cand.id
			return forwardStream(ctx, opts, first, src, func(err error) {
				if isProviderUnhealthy(err) {
					fc.markUnhealthyID(id)
				}
			}), nil
		}
	}

	return nil, allClientsFailedError(lastErr)
}

func allClientsFailedError(err error) error {
	if err == nil {
		return ErrAllClientsFailed
	}
	return fmt.Errorf("%w: %w", ErrAllClientsFailed, err)
}

// fireFallback invokes the onFallback callback (if set) for a transition from the
// candidate at position pos to the next candidate. Positions and clients come
// from the operation's snapshot, so the reported indices are internally
// consistent regardless of concurrent chain mutation.
func (fc *FallbackChain) fireFallback(pos int, candidates []fallbackCandidate, err error) {
	if fc.onFallback == nil || pos >= len(candidates)-1 {
		return
	}
	from := candidates[pos]
	to := candidates[pos+1]
	fc.onFallback(from.pos, to.pos, from.client, to.client, err)
}

// drainStream consumes a stream to completion so its producer goroutine is not
// left blocked when the chain abandons it to fail over.
func drainStream(src <-chan llms.StreamChunk) {
	for range src { //nolint:revive // intentionally draining to unblock the producer
	}
}

// forwardStream emits first, then forwards src to a new channel with StreamSender
// timeout protection and the terminal-chunk guarantee. onDone (if set) is invoked
// once with the terminal stream error, if one was observed.
func forwardStream(ctx context.Context, opts *llms.CallOptions, first llms.StreamChunk, src <-chan llms.StreamChunk, onDone func(error)) <-chan llms.StreamChunk {
	out := make(chan llms.StreamChunk, llms.GetBufferSize(opts))
	sender := llms.NewStreamSender(ctx, out, opts.StreamSendTimeout)

	go func() {
		defer close(out)
		termErr := first.Error
		if onDone != nil {
			defer func() { onDone(termErr) }()
		}
		defer sender.EnsureTerminal()

		if sender.ForwardTerminalOnEarlyExit(sender.Send(first)) {
			return
		}
		for chunk := range src {
			if chunk.Error != nil {
				termErr = chunk.Error
			}
			if sender.ForwardTerminalOnEarlyExit(sender.Send(chunk)) {
				return
			}
		}
	}()

	return out
}

// Provider returns the first client's provider
func (fc *FallbackChain) Provider() llms.Provider {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	if len(fc.entries) == 0 {
		return ""
	}
	return fc.entries[0].client.Provider()
}

// Model returns the first client's model
func (fc *FallbackChain) Model() string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	if len(fc.entries) == 0 {
		return ""
	}
	return fc.entries[0].client.Model()
}

// Clients returns all clients in the chain
func (fc *FallbackChain) Clients() []llms.LLM {
	return fc.clientsSnapshot()
}

func (fc *FallbackChain) clientsSnapshot() []llms.LLM {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	result := make([]llms.LLM, len(fc.entries))
	for i := range fc.entries {
		result[i] = fc.entries[i].client
	}
	return result
}

// snapshot captures a point-in-time view of the chain under a single lock so
// clients, their positions, and their health can never drift apart. It returns
// the eligible (currently-healthy) candidates in order; if none are eligible it
// returns every candidate so the chain still attempts a call. Callers iterate
// the returned candidates and record health via markHealthyID/markUnhealthyID,
// which bind the result to the client's stable id regardless of any concurrent
// AddClient/RemoveClient.
func (fc *FallbackChain) snapshot() []fallbackCandidate {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	now := fc.now()
	all := make([]fallbackCandidate, len(fc.entries))
	eligible := make([]fallbackCandidate, 0, len(fc.entries))
	for i := range fc.entries {
		cand := fallbackCandidate{pos: i, id: fc.entries[i].id, client: fc.entries[i].client}
		all[i] = cand
		if isHealthyAt(fc.entries[i].unhealthyUntil, now) {
			eligible = append(eligible, cand)
		}
	}
	if len(eligible) > 0 {
		return eligible
	}
	return all
}

// AddClient adds a client to the end of the chain
func (fc *FallbackChain) AddClient(client llms.LLM) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.entries = append(fc.entries, fallbackEntry{id: fc.nextID, client: client})
	fc.nextID++
}

// RemoveClient removes a client from the chain by index
func (fc *FallbackChain) RemoveClient(idx int) bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if idx < 0 || idx >= len(fc.entries) {
		return false
	}

	// Removing the entry drops its health with it; remaining clients keep their
	// stable ids, so any in-flight operation still records health correctly.
	fc.entries = append(fc.entries[:idx], fc.entries[idx+1:]...)

	return true
}

// SetClientHealthy manually sets a client's health status.
// Does nothing if idx is out of bounds.
func (fc *FallbackChain) SetClientHealthy(idx int, healthy bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if idx >= 0 && idx < len(fc.entries) {
		if healthy {
			fc.entries[idx].unhealthyUntil = time.Time{}
			return
		}
		fc.entries[idx].unhealthyUntil = fc.now().Add(fc.recoveryAfter)
	}
}

// IsClientHealthy returns whether a client is considered healthy
func (fc *FallbackChain) IsClientHealthy(idx int) bool {
	return fc.isHealthy(idx)
}

// ResetHealth marks all clients as healthy
func (fc *FallbackChain) ResetHealth() {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	for i := range fc.entries {
		fc.entries[i].unhealthyUntil = time.Time{}
	}
}

func (fc *FallbackChain) isHealthy(idx int) bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	// Out of bounds indices are considered healthy (shouldn't happen in practice)
	if idx < 0 || idx >= len(fc.entries) {
		return true
	}
	return isHealthyAt(fc.entries[idx].unhealthyUntil, fc.now())
}

// markHealthyID clears the health cooldown for the client with the given stable
// id. It is a no-op if that client has since been removed from the chain.
func (fc *FallbackChain) markHealthyID(id int64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if i := fc.indexOfID(id); i >= 0 {
		fc.entries[i].unhealthyUntil = time.Time{}
	}
}

// markUnhealthyID puts the client with the given stable id into cooldown. It is
// a no-op if that client has since been removed from the chain, so a failure can
// never be misattributed to whichever client now occupies the old position.
func (fc *FallbackChain) markUnhealthyID(id int64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if i := fc.indexOfID(id); i >= 0 {
		fc.entries[i].unhealthyUntil = fc.now().Add(fc.recoveryAfter)
	}
}

// indexOfID returns the current position of the entry with the given stable id,
// or -1 if no such entry exists. Callers must hold fc.mu.
func (fc *FallbackChain) indexOfID(id int64) int {
	for i := range fc.entries {
		if fc.entries[i].id == id {
			return i
		}
	}
	return -1
}

func isHealthyAt(unhealthyUntil time.Time, now time.Time) bool {
	return unhealthyUntil.IsZero() || !now.Before(unhealthyUntil)
}

// Ensure FallbackChain implements LLM
var _ llms.LLM = (*FallbackChain)(nil)

// WeightedFallbackChain extends FallbackChain with weighted selection
type WeightedFallbackChain struct {
	*FallbackChain
	weights []int
}

// NewWeightedFallbackChain creates a fallback chain with weighted priorities.
// Higher weights are tried first.
//
// Parameters:
//   - clients: List of LLM clients (must not be empty)
//   - weights: Optional weights for each client. If empty, all clients get weight 1.
//     If provided, must have same length as clients.
//   - opts: Optional fallback configuration options
//
// Returns error if validation fails. Use MustNewWeightedFallbackChain for panic on error.
func NewWeightedFallbackChain(clients []llms.LLM, weights []int, opts ...FallbackOption) (*WeightedFallbackChain, error) {
	if len(clients) == 0 {
		return nil, ErrNoClientsAvailable
	}

	// Validate weights if provided
	if len(weights) > 0 && len(weights) != len(clients) {
		return nil, fmt.Errorf("weights length (%d) must match clients length (%d) or be empty", len(weights), len(clients))
	}

	// Sort clients by weight (descending)
	type clientWeight struct {
		client llms.LLM
		weight int
	}

	cw := make([]clientWeight, len(clients))
	for i := range clients {
		w := 1
		if len(weights) > 0 {
			w = weights[i]
			if w < 0 {
				return nil, fmt.Errorf("weight at index %d is negative: %d", i, w)
			}
		}
		cw[i] = clientWeight{client: clients[i], weight: w}
	}

	// Sort by weight descending (higher weights first)
	sort.Slice(cw, func(i, j int) bool {
		return cw[i].weight > cw[j].weight
	})

	sortedClients := make([]llms.LLM, len(cw))
	sortedWeights := make([]int, len(cw))
	for i, c := range cw {
		sortedClients[i] = c.client
		sortedWeights[i] = c.weight
	}

	return &WeightedFallbackChain{
		FallbackChain: NewFallbackChain(sortedClients, opts...),
		weights:       sortedWeights,
	}, nil
}

// MustNewWeightedFallbackChain is like NewWeightedFallbackChain but panics on error.
// Use this when you're certain the inputs are valid.
func MustNewWeightedFallbackChain(clients []llms.LLM, weights []int, opts ...FallbackOption) *WeightedFallbackChain {
	wfc, err := NewWeightedFallbackChain(clients, weights, opts...)
	if err != nil {
		panic(fmt.Sprintf("MustNewWeightedFallbackChain: %v", err))
	}
	return wfc
}

// Weights returns the weights in priority order
func (wfc *WeightedFallbackChain) Weights() []int {
	result := make([]int, len(wfc.weights))
	copy(result, wfc.weights)
	return result
}
