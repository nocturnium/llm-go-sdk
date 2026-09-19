package llms

import (
	"context"
	"errors"
	"testing"
)

// TestStreamError_IsDoesNotClaimEveryTarget pins that a StreamError reports
// only what it is. Its Is method used to ask whether ErrStreamInterrupted
// matched the target, which holds for every target, so IsTemporary told
// third-party retry logic to retry a stream the user had canceled.
func TestStreamError_IsDoesNotClaimEveryTarget(t *testing.T) {
	err := &StreamError{Cause: context.Canceled}

	if !errors.Is(err, ErrStreamInterrupted) {
		t.Error("a StreamError no longer matches ErrStreamInterrupted")
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("a StreamError no longer matches its own cause")
	}
	if errors.Is(err, ErrRateLimited) {
		t.Error("a StreamError claims to be a rate-limit error")
	}
	if IsTemporary(err) {
		t.Error("IsTemporary says a canceled stream is worth retrying")
	}
}
