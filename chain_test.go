package llms

import "testing"

// labeledWrapper is a middleware wrapper that records a label, so tests can assert
// the order in which Chain applies middleware.
type labeledWrapper struct {
	LLM
	label string
}

func (w labeledWrapper) Unwrap() LLM { return w.LLM }

func labelMiddleware(label string) Middleware {
	return func(next LLM) LLM { return labeledWrapper{LLM: next, label: label} }
}

func TestChain_Empty(t *testing.T) {
	base := NewMockLLM()
	if got := Chain(base); got != LLM(base) {
		t.Errorf("Chain(base) with no middleware should return base unchanged")
	}
}

func TestChain_Single(t *testing.T) {
	base := NewMockLLM()
	out := Chain(base, labelMiddleware("a"))
	lw, ok := out.(labeledWrapper)
	if !ok || lw.label != "a" {
		t.Fatalf("Chain applied wrong/zero middleware: %#v", out)
	}
	if UnwrapAll(out) != LLM(base) {
		t.Error("UnwrapAll(chained) should reach the base LLM")
	}
}

func TestChain_OrderAndNilSkip(t *testing.T) {
	base := NewMockLLM()
	// First listed is innermost, last listed is outermost; nil is skipped.
	out := Chain(base, labelMiddleware("inner"), nil, labelMiddleware("outer"))

	mws := GetMiddleware(out) // outermost first, innermost last
	if len(mws) != 2 {
		t.Fatalf("expected 2 wrappers (nil skipped), got %d", len(mws))
	}
	if outer, ok := mws[0].(labeledWrapper); !ok || outer.label != "outer" {
		t.Errorf("outermost = %#v, want label \"outer\"", mws[0])
	}
	if inner, ok := mws[1].(labeledWrapper); !ok || inner.label != "inner" {
		t.Errorf("innermost = %#v, want label \"inner\"", mws[1])
	}
	if UnwrapAll(out) != LLM(base) {
		t.Error("UnwrapAll should reach the base provider through the chain")
	}
}

// selfWrapper returns itself from Unwrap, producing a cycle. It carries a slice
// so the wrapper value is non-comparable, exercising the fallback identity used
// when a wrapper cannot be used as its own map key.
type selfWrapper struct {
	LLM
	//lint:ignore U1000 keeps the wrapper non-comparable for the fallback-identity test
	pad []int
}

func (w *selfWrapper) Unwrap() LLM { return w }

func TestUnwrapAll_CyclicNonComparableWrapper(t *testing.T) {
	w := &selfWrapper{LLM: NewMockLLM()}
	if got := UnwrapAll(w); got != LLM(w) {
		t.Errorf("UnwrapAll(cyclic) = %v, want the wrapper itself", got)
	}
	if mws := GetMiddleware(w); len(mws) != 1 {
		t.Errorf("GetMiddleware(cyclic) returned %d wrappers, want 1", len(mws))
	}
}

// typedNilWrapper is a value-receiver wrapper; calling Unwrap on a nil pointer
// to it panics, which is exactly the case a typed-nil interface hides.
type typedNilWrapper struct{ LLM }

func (typedNilWrapper) Unwrap() LLM { return nil }

func TestUnwrapAll_TypedNilWrapper(t *testing.T) {
	var wrapper *typedNilWrapper
	if got := UnwrapAll(wrapper); got != nil {
		t.Errorf("UnwrapAll(typed nil) = %v, want nil", got)
	}
	if got := GetMiddleware(wrapper); len(got) != 0 {
		t.Errorf("GetMiddleware(typed nil) returned %d wrappers, want 0", len(got))
	}
}
