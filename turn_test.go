package llms

import "testing"

func TestCurrentTurnStart(t *testing.T) {
	u := Message{Role: RoleUser}
	a := Message{Role: RoleAssistant}
	tool := Message{Role: RoleTool}
	sys := Message{Role: RoleSystem}
	tests := []struct {
		name string
		msgs []Message
		want int
	}{
		{"empty", nil, 0},
		{"ends on user", []Message{u, a, u}, 3},
		{"tool loop in progress", []Message{u, a, u, a, tool, a, tool}, 3},
		{"only assistant side", []Message{a, tool}, 0},
		{"system then turn", []Message{sys, a}, 1},
	}
	for _, tt := range tests {
		if got := CurrentTurnStart(tt.msgs); got != tt.want {
			t.Errorf("%s: CurrentTurnStart = %d, want %d", tt.name, got, tt.want)
		}
	}
}
