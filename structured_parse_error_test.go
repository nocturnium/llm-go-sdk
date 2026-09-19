package llms

import (
	"strings"
	"testing"
)

// TestParseTyped_ReportsTheFieldThatFailed pins that a type mismatch names the
// field and the types involved. The parse error used to be a constant, so the
// repair turn told the model its valid JSON was "not valid JSON" and named
// nothing for it to fix.
func TestParseTyped_ReportsTheFieldThatFailed(t *testing.T) {
	type person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	_, err := parseTyped[person](`{"name":"ada","age":"thirty"}`)
	if err == nil {
		t.Fatal("a string in an int field parsed without error")
	}
	for _, want := range []string{"age", "string", "int"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	if prompt := repairPrompt(err); !strings.Contains(prompt, "age") {
		t.Errorf("repair prompt names no field: %q", prompt)
	}
}
