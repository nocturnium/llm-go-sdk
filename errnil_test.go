package llms

import (
	"errors"
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
