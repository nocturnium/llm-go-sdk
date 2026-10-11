package llmadk

import (
	"encoding/json"

	"context"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestBridgeHandler_PreservesStringKeyedMaps(t *testing.T) {
	type named map[string]int
	for name, tc := range map[string]struct {
		in   any
		want map[string]any
	}{
		"map[string]any":    {map[string]any{"a": 1}, map[string]any{"a": 1}},
		"map[string]string": {map[string]string{"a": "b"}, map[string]any{"a": "b"}},
		"named map":         {named{"n": 2}, map[string]any{"n": 2}},
		"scalar":            {5, map[string]any{"result": 5}},
		"int-keyed map":     {map[int]string{1: "x"}, map[string]any{"result": map[int]string{1: "x"}}},
	} {
		t.Run(name, func(t *testing.T) {
			h := bridgeHandler(llms.ToolHandler(func(context.Context, json.RawMessage) (any, error) { return tc.in, nil }))
			got, err := h(nil, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}
