package entity

import (
	"sync"
	"testing"
)

// TestPubSub_PublishVsUnsubscribeChurn reproduces the #262 interleaving
// under the race detector: PublishEvent snapshots the subscriber list
// under RLock and sends after releasing it, while Unsubscribe used to
// close() the channel under Lock — a send-on-closed-channel panic as
// soon as a publish raced an unsubscribe (e.g. OTLP strategy shutdown).
func TestPubSub_PublishVsUnsubscribeChurn(t *testing.T) {
	t.Cleanup(resetEventChannelForTest)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Publish storm.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					PublishEvent(Event{})
				}
			}
		}()
	}

	// Subscribe/unsubscribe churn.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			ch := SubscribeEvents(4)
			UnsubscribeEvents(ch)
		}
		close(stop)
	}()

	wg.Wait()
}
