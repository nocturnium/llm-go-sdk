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

	mws := GetMiddleware(out) // outermost -> innermost
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
