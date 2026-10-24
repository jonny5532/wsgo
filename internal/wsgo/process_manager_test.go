package wsgo

import (
	"sync"
	"testing"
	"time"
)

// TestRunProcessHandlesStartFailure checks that a child process which can't
// even be executed (so ProcessState is nil) doesn't panic the manager.
func TestRunProcessHandlesStartFailure(t *testing.T) {
	oldExecutable := pythonExecutable
	pythonExecutable = "/nonexistent/wsgo-test-binary"
	defer func() { pythonExecutable = oldExecutable }()

	processManagerStopping = false
	defer func() { processManagerStopping = false }()

	oldCommands := processCommands
	processCommands = nil
	defer func() { processCommands = oldCommands }()

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer close(done)
		RunProcess(&wg, 1)
	}()

	select {
	case <-done:
		// Good - a failed exec must not panic or loop forever.
	case <-time.After(5 * time.Second):
		t.Fatal("RunProcess did not return after failing to start a process")
	}

	processCommandsMutex.Lock()
	defer processCommandsMutex.Unlock()
	for _, cmd := range processCommands {
		if cmd.Process == nil {
			t.Fatal("failed-to-start command left in processCommands")
		}
	}
}
