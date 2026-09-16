package llms

import (
	"strings"
	"testing"
)

// TestVersionInfo pins the in-package defaults and the shape VersionInfo formats
// them into. It says nothing about whether the release ldflags reach these
// variables: that wiring is only observable in a built release binary.
func TestVersionInfo(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
	if Commit == "" {
		t.Fatal("Commit must not be empty")
	}
	if Date == "" {
		t.Fatal("Date must not be empty")
	}

	info := VersionInfo()
	if !strings.HasPrefix(info, "llms ") {
		t.Errorf("VersionInfo() = %q, want prefix %q", info, "llms ")
	}
	for _, want := range []string{Version, Commit, Date} {
		if !strings.Contains(info, want) {
			t.Errorf("VersionInfo() = %q, want it to contain %q", info, want)
		}
	}
}
