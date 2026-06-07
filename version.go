package llms

import "fmt"

// Version information set by build flags.
var (
	// Version is the semantic version of this package.
	Version = "0.0.0-dev"

	// Commit is the git commit hash.
	Commit = "unknown"

	// Date is the build date.
	Date = "unknown"
)

// VersionInfo returns version information as a formatted string.
func VersionInfo() string {
	return fmt.Sprintf("llms %s (commit: %s, built: %s)", Version, Commit, Date)
}
