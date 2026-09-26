package llmadk

import "testing"

// TestFixtures replays every recorded provider fixture offline through the
// real provider clients and the bridge. Scenarios without a recording skip.
func TestFixtures(t *testing.T) {
	runScenarios(t, replayClient)
}
