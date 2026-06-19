package resilience

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
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

// FallbackChain tries multiple LLM clients in order until one succeeds
type FallbackChain struct {
	mu       sync.RWMutex
	clients  []llms.LLM
	selector FallbackSelector

	// Callbacks
	onFallback func(fromIdx int, toIdx int, from, to llms.LLM, err error)
	onSuccess  func(idx int, client llms.LLM)

	// Health tracking - using a slice for simpler index management.
	// Each index corresponds directly to the client at the same index.
	// Zero time = healthy, non-zero future time = unhealthy until that instant.
	// Protected by mu.
	unhealthyUntil []time.Time
	recoveryAfter  time.Duration
	now            func() time.Time
}

// FallbackOption configures a FallbackChain
type FallbackOption func(*FallbackChain)

// NewFallbackChain creates a new fallback chain with the given clients
func NewFallbackChain(clients []llms.LLM, opts ...FallbackOption) *FallbackChain {
	fc := &FallbackChain{
		clients:        clients,
		selector:       DefaultFallbackSelector{},
		unhealthyUntil: make([]time.Time, len(clients)),
		recoveryAfter:  defaultFallbackRecoveryAfter,
		now:            time.Now,
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
	fc.mu.RLock()
	clients := fc.clients
	fc.mu.RUnlock()

	if len(clients) == 0 {
		return "", ErrNoClientsAvailable
	}

	candidateIndexes := fc.candidateIndexes(len(clients))

	var lastErr error
	for pos, i := range candidateIndexes {
		client := clients[i]
		result, err := llms.Call(ctx, client, prompt, options...)
		if err == nil {
			fc.markHealthy(i)
			if fc.onSuccess != nil {
				fc.onSuccess(i, client)
			}
			return result, nil
		}

		lastErr = err
		if isProviderUnhealthy(err) {
			fc.markUnhealthy(i)
		}

		// Check if we should fallback
		if !fc.selector.ShouldFallback(err) {
			return "", err
		}

		// Callback for fallback
		if fc.onFallback != nil && pos < len(candidateIndexes)-1 {
			nextIdx := candidateIndexes[pos+1]
			fc.onFallback(i, nextIdx, client, clients[nextIdx], err)
		}
	}

	return "", allClientsFailedError(lastErr)
}

// executeWithFallback executes an operation across clients with fallback logic.
// The operation function receives a client and returns a result and error.
// This helper centralizes the retry/fallback logic used by GenerateContent and Stream.
func executeWithFallback[T any](fc *FallbackChain, operation func(client llms.LLM) (T, error)) (T, error) {
	fc.mu.RLock()
	clients := fc.clients
	fc.mu.RUnlock()

	var zero T
	if len(clients) == 0 {
		return zero, ErrNoClientsAvailable
	}

	candidateIndexes := fc.candidateIndexes(len(clients))

	var lastErr error
	for pos, i := range candidateIndexes {
		client := clients[i]
		result, err := operation(client)
		if err == nil {
			fc.markHealthy(i)
			if fc.onSuccess != nil {
				fc.onSuccess(i, client)
			}
			return result, nil
		}

		lastErr = err
		if isProviderUnhealthy(err) {
			fc.markUnhealthy(i)
		}

		// Check if we should fallback
		if !fc.selector.ShouldFallback(err) {
			return zero, err
		}

		// Callback for fallback
		if fc.onFallback != nil && pos < len(candidateIndexes)-1 {
			nextIdx := candidateIndexes[pos+1]
			fc.onFallback(i, nextIdx, client, clients[nextIdx], err)
		}
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
	fc.mu.RLock()
	clients := fc.clients
	fc.mu.RUnlock()
	if len(clients) == 0 {
		return nil, ErrNoClientsAvailable
	}

	opts := llms.ApplyOptions(options...)
	candidates := fc.candidateIndexes(len(clients))
	var lastErr error

	for pos, i := range candidates {
		client := clients[i]
		src, err := client.Stream(ctx, messages, options...)
		if err != nil {
			lastErr = err
			if isProviderUnhealthy(err) {
				fc.markUnhealthy(i)
			}
			if !fc.selector.ShouldFallback(err) {
				return nil, err
			}
			fc.fireFallback(pos, candidates, clients, err)
			continue
		}

		first, ok := <-src
		switch {
		case !ok:
			// Stream closed without any chunk; treat as an (empty) success.
			fc.markHealthy(i)
			return forwardStream(ctx, opts, llms.StreamChunk{Done: true}, src, nil), nil
		case first.Error != nil:
			lastErr = first.Error
			if isProviderUnhealthy(first.Error) {
				fc.markUnhealthy(i)
			}
			if !fc.selector.ShouldFallback(first.Error) {
				// Committed to this client by contract (chan already returned); deliver its error.
				return forwardStream(ctx, opts, first, src, nil), nil
			}
			fc.fireFallback(pos, candidates, clients, first.Error)
			go drainStream(src) // let the abandoned producer finish without blocking
			continue
		default:
			fc.markHealthy(i)
			if fc.onSuccess != nil {
				fc.onSuccess(i, client)
			}
			idx := i
			return forwardStream(ctx, opts, first, src, func(err error) {
				if isProviderUnhealthy(err) {
					fc.markUnhealthy(idx)
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
// candidate at position pos to the next candidate.
func (fc *FallbackChain) fireFallback(pos int, candidates []int, clients []llms.LLM, err error) {
	if fc.onFallback == nil || pos >= len(candidates)-1 {
		return
	}
	from := candidates[pos]
	to := candidates[pos+1]
	fc.onFallback(from, to, clients[from], clients[to], err)
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

	if len(fc.clients) == 0 {
		return ""
	}
	return fc.clients[0].Provider()
}

// Model returns the first client's model
func (fc *FallbackChain) Model() string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	if len(fc.clients) == 0 {
		return ""
	}
	return fc.clients[0].Model()
}

// Clients returns all clients in the chain
func (fc *FallbackChain) Clients() []llms.LLM {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	result := make([]llms.LLM, len(fc.clients))
	copy(result, fc.clients)
	return result
}

// AddClient adds a client to the end of the chain
func (fc *FallbackChain) AddClient(client llms.LLM) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.clients = append(fc.clients, client)
	fc.unhealthyUntil = append(fc.unhealthyUntil, time.Time{})
}

// RemoveClient removes a client from the chain by index
func (fc *FallbackChain) RemoveClient(idx int) bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if idx < 0 || idx >= len(fc.clients) {
		return false
	}

	// Remove client and corresponding health status
	fc.clients = append(fc.clients[:idx], fc.clients[idx+1:]...)
	fc.unhealthyUntil = append(fc.unhealthyUntil[:idx], fc.unhealthyUntil[idx+1:]...)

	return true
}

// SetClientHealthy manually sets a client's health status.
// Does nothing if idx is out of bounds.
func (fc *FallbackChain) SetClientHealthy(idx int, healthy bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if idx >= 0 && idx < len(fc.unhealthyUntil) {
		if healthy {
			fc.unhealthyUntil[idx] = time.Time{}
			return
		}
		fc.unhealthyUntil[idx] = fc.now().Add(fc.recoveryAfter)
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

	for i := range fc.unhealthyUntil {
		fc.unhealthyUntil[i] = time.Time{}
	}
}

func (fc *FallbackChain) candidateIndexes(clientCount int) []int {
	fc.mu.RLock()
	now := fc.now()
	eligible := make([]int, 0, clientCount)
	all := make([]int, 0, clientCount)
	for i := 0; i < clientCount; i++ {
		all = append(all, i)
		if i >= len(fc.unhealthyUntil) || isHealthyAt(fc.unhealthyUntil[i], now) {
			eligible = append(eligible, i)
		}
	}
	fc.mu.RUnlock()

	if len(eligible) > 0 {
		return eligible
	}
	return all
}

func (fc *FallbackChain) isHealthy(idx int) bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	// Out of bounds indices are considered healthy (shouldn't happen in practice)
	if idx < 0 || idx >= len(fc.unhealthyUntil) {
		return true
	}
	return isHealthyAt(fc.unhealthyUntil[idx], fc.now())
}

func (fc *FallbackChain) markHealthy(idx int) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if idx >= 0 && idx < len(fc.unhealthyUntil) {
		fc.unhealthyUntil[idx] = time.Time{}
	}
}

func (fc *FallbackChain) markUnhealthy(idx int) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if idx >= 0 && idx < len(fc.unhealthyUntil) {
		fc.unhealthyUntil[idx] = fc.now().Add(fc.recoveryAfter)
	}
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
