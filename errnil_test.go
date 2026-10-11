package llms

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestAPIError_IsNilTarget(t *testing.T) {
	if errors.Is(&APIError{}, nil) {
		t.Error("errors.Is(unclassified APIError, nil) = true, want false")
	}
	if errors.Is(&APIError{StatusCode: 429}, nil) {
		t.Error("errors.Is(classified APIError, nil) = true, want false")
	}
	if !errors.Is(&APIError{StatusCode: 429}, ErrRateLimited) {
		t.Error("classified APIError must still match its sentinel")
	}
}

func TestGetErrorDetails_RootCauseThroughMultiWrap(t *testing.T) {
	inner := errors.New("inner")
	multi := fmt.Errorf("%w: %w", context.Canceled, inner)
	if got := GetErrorDetails(multi).RootCause; got != context.Canceled {
		t.Errorf("RootCause = %v, want the first wrapped error context.Canceled", got)
	}
	if got := GetErrorDetails(inner).RootCause; got != inner {
		t.Errorf("RootCause of an unwrapped error = %v, want itself", got)
	}
	nested := fmt.Errorf("outer: %w", fmt.Errorf("mid: %w", inner))
	if got := GetErrorDetails(nested).RootCause; got != inner {
		t.Errorf("RootCause = %v, want inner", got)
	}
}
