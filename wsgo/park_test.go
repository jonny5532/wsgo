package wsgo

import (
	"testing"
)

// TestParkedNotificationNotLost checks that a notification delivered in the
// window between a parked job being registered and its waiter starting to
// select on the notify channel is not dropped.
func TestParkedNotificationNotLost(t *testing.T) {
	park := newParkedJob(nil)

	parkedJobMutex.Lock()
	parkedJobs["test-notify-channel"] = []*ParkedJob{park}
	parkedJobMutex.Unlock()
	defer func() {
		parkedJobMutex.Lock()
		delete(parkedJobs, "test-notify-channel")
		parkedJobMutex.Unlock()
	}()

	// Notify before anyone is waiting on the channel, as happens when
	// go_notify_parked races with the start of ParkJob's select.
	NotifyParked("test-notify-channel", RETRY, "")

	select {
	case notification := <-park.notify:
		if notification.action != RETRY {
			t.Fatalf("got action %d, want RETRY", notification.action)
		}
	default:
		t.Fatal("parked notification was lost before the waiter started")
	}
}
