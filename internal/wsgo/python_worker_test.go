package wsgo

import "testing"

// TestAllWorkersAreStuckNoWorkers checks the vacuous-truth case: with no
// workers configured, there is no reason to declare the process stuck.
func TestAllWorkersAreStuckNoWorkers(t *testing.T) {
	oldWorkers := workers
	workers = nil
	defer func() { workers = oldWorkers }()

	if AllWorkersAreStuck() {
		t.Fatal("AllWorkersAreStuck returned true with no workers configured")
	}
}
