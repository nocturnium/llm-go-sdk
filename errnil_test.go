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

func TestProviderFromError_SearchesAllMultiWrapBranches(t *testing.T) {
	empty := &ProviderError{Err: errors.New("op")}
	named := &ProviderError{Provider: ProviderOpenAI, Err: errors.New("e")}
	if got := ProviderFromError(fmt.Errorf("%w: %w", empty, named)); got != ProviderOpenAI {
		t.Errorf("ProviderFromError = %q, want %q from the second branch", got, ProviderOpenAI)
	}
	if got := ProviderFromError(errors.Join(errors.New("x"), named)); got != ProviderOpenAI {
		t.Errorf("ProviderFromError(Join) = %q, want %q", got, ProviderOpenAI)
	}
	if got := ProviderFromError(empty); got != "" {
		t.Errorf("ProviderFromError(empty) = %q, want empty", got)
	}
}

func TestParseTyped_BareScalarNeedsWordBoundaries(t *testing.T) {
	for _, s := range []string{"falsehood", "trueish", "untrue", "nullable", "v5"} {
		if _, err := parseTyped[bool](s); err == nil {
			if _, err := parseTyped[int](s); err == nil {
				t.Errorf("%q parsed as a scalar, want an error", s)
			}
		}
	}
	for s, want := range map[string]bool{"false.": false, "The answer is true.": true, "(true)": true} {
		got, err := parseTyped[bool](s)
		if err != nil || got != want {
			t.Errorf("parseTyped[bool](%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	if got, err := parseTyped[float64]("about 3.14."); err != nil || got != 3.14 {
		t.Errorf("parseTyped[float64] = %v, %v; want 3.14", got, err)
	}
}
