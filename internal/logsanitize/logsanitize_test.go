package logsanitize

import (
	"strings"
	"testing"
	"unicode"
)

func TestValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"newline", "line1\nline2", `line1\nline2`},
		{"carriage_return", "a\rb", `a\rb`},
		{"crlf", "a\r\nb", `a\r\nb`},
		{"tab", "a\tb", `a\tb`},
		{"nul", "a\x00b", `a\x00b`},
		{"escape", "a\x1bb", `a\x1bb`},
		{"del", "a\x7fb", `a\x7fb`},
		{"unicode_control", "a\u0085b", `a\u0085b`},
		{"invalid_utf8", "a\n\xffb", "a\\n\xffb"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Value(tt.in)
			if got != tt.want {
				t.Fatalf("Value(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if containsControl(got) {
				t.Fatalf("Value(%q) = %q still contains a raw control character", tt.in, got)
			}
		})
	}
}

func TestValueNeutralizesForgedLogLines(t *testing.T) {
	got := Value("ok\r\nERROR forged log line\x1b[31m")
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("Value returned raw CR/LF: %q", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("Value returned raw escape character: %q", got)
	}
	want := `ok\r\nERROR forged log line\x1b[31m`
	if got != want {
		t.Fatalf("Value forged line = %q, want %q", got, want)
	}
}

func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
