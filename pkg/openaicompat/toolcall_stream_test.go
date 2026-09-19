package openaicompat

import (
	"testing"
)

// TestToolCalls_SparseIndexLeavesNoNilFunction pins that a stream opening at a
// non-zero index does not hand the caller placeholder entries. The back-fill
// created zero-value calls whose Function was nil, so reading Function.Name on
// one panicked.
func TestToolCalls_SparseIndexLeavesNoNilFunction(t *testing.T) {
	two := 2
	calls, err := appendOrMergeToolCall(nil, ToolCall{Index: &two, ID: "call_c", Function: &FunctionCall{Name: "lookup"}})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	compacted := compactToolCalls(calls)
	if len(compacted) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(compacted), compacted)
	}
	for i, call := range compacted {
		if call.Function == nil {
			t.Errorf("call %d has a nil Function", i)
		}
	}
}

// TestToolCalls_IndexlessContinuationMergesIntoLast pins that an argument
// delta carrying neither an index nor an id continues the call in flight
// instead of starting a second one with half the argument JSON.
func TestToolCalls_IndexlessContinuationMergesIntoLast(t *testing.T) {
	calls, err := appendOrMergeToolCall(nil, ToolCall{ID: "call_1", Type: "function", Function: &FunctionCall{Name: "lookup", Arguments: `{"q":`}})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	calls, err = appendOrMergeToolCall(calls, ToolCall{Function: &FunctionCall{Arguments: `"go"}`}})
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(calls), calls)
	}
	if got := calls[0].Function.Arguments; got != `{"q":"go"}` {
		t.Errorf("Arguments = %q, want %q", got, `{"q":"go"}`)
	}
}
