package agentstate

import (
	"sync"
	"testing"
)

// TestLogPubSub_PublishVsUnsubscribeChurn — same #262 interleaving as
// the entity channel: publish storm concurrent with
// subscribe/unsubscribe churn must never panic on a closed channel.
func TestLogPubSub_PublishVsUnsubscribeChurn(t *testing.T) {
	t.Cleanup(resetLogChannelForTest)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					PublishLog(LogRecord{Body: "storm"})
				}
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			ch := SubscribeLogs(4)
			UnsubscribeLogs(ch)
		}
		close(stop)
	}()

	wg.Wait()
}
