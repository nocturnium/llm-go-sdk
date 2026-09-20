package httpclient

import (
	"strings"
	"testing"
)

// TestSanitizeModelName_TraversalSurvivesNoReassembly pins that traversal
// sequences cannot be rebuilt by the sanitizer itself. Control characters were
// stripped after the ".." removal, so ".\x00." closed back up into "..", and a
// single pass let "....//" collapse into one.
func TestSanitizeModelName_TraversalSurvivesNoReassembly(t *testing.T) {
	for _, input := range []string{
		".\x00./.\x00./etc/passwd",
		"....//....//etc/passwd",
		"..\x7f./secret",
	} {
		got := SanitizeModelName(input)
		if strings.Contains(got, "..") {
			t.Errorf("SanitizeModelName(%q) = %q, which still contains \"..\"", input, got)
		}
		if strings.ContainsAny(got, "/\\") {
			t.Errorf("SanitizeModelName(%q) = %q, which still contains a separator", input, got)
		}
	}
}
