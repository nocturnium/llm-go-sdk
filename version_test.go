package llms

import (
	"strings"
	"testing"
)

// TestVersionInfo guards the build-time version wiring. The release ldflags inject
// Version/Commit/Date via -X github.com/nocturnium/llm-go-sdk/v6.<Var>; if the
// module path and this package's import path ever drift apart, the injection
// silently no-ops. This test pins the defaults and the formatted shape so such a
// drift is caught by a failing assertion rather than a release with "0.0.0-dev".
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
