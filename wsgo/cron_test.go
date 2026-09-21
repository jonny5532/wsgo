package wsgo

import (
	"sync"
	"testing"
)

// TestCronConcurrentAddAndTick exercises runtime cron registration (which an
// application may do at any time) concurrently with the cron runner. Run with
// -race to detect unsynchronised access to the crons slice.
func TestCronConcurrentAddAndTick(t *testing.T) {
	cronsMutex.Lock()
	oldCrons := crons
	crons = nil
	cronsMutex.Unlock()
	defer func() {
		cronsMutex.Lock()
		crons = oldCrons
		cronsMutex.Unlock()
	}()

	// CronTick sends due jobs to an unbuffered channel; drain it so the
	// tick itself doesn't block.
	drainDone := make(chan struct{})
	go func() {
		for {
			select {
			case <-backgroundJobs:
			case <-drainDone:
				return
			}
		}
	}()
	defer close(drainDone)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				addCron(nil, "test", 1, -1, -1, -1, -1, -1)
				CronTick()
				_ = DurationUntilNextCron()
			}
		}()
	}
	wg.Wait()
}
