package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestClose_CancelsInFlightHandlers pins that Close releases a handler waiting
// on its context. Handlers used to derive from a context nothing canceled, so
// a handler parked waiting for a person, which the sampling and elicitation
// docs instruct, leaked along with its semaphore slot.
func TestClose_CancelsInFlightHandlers(t *testing.T) {
	entered := make(chan struct{})
	released := make(chan error, 1)

	m := newMockTransport()
	c := clientWithHandlers(t, m, map[string]requestHandler{
		"sampling/createMessage": func(ctx context.Context, _ json.RawMessage) (any, error) {
			close(entered)
			<-ctx.Done()
			released <- ctx.Err()
			return nil, nil
		},
	})

	c.dispatchRequest(requestFrame("1", "sampling/createMessage", map[string]any{}), json.RawMessage(`"1"`))

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}

	_ = c.Close()

	select {
	case err := <-released:
		if err == nil {
			t.Error("handler context carried no error after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the in-flight handler")
	}
}
