package otlp

import (
	"context"
	"sync"
	"testing"
	"time"
)

// A collector that drops packets rather than refusing them leaves an
// export in flight until its own timeout, a minute by default. Shutdown
// used to wait for it, spending the whole stop budget the service
// manager gives the agent: measured at fifty to fifty-six seconds on a
// Linux host against a black-holed address, where a refused connection
// stopped in ten.
func TestShutdownAbortsAPushInFlight(t *testing.T) {
	s := &OTLPSyncStrategy{}
	s.pushDone = make(chan struct{})
	pushCtx, cancel := context.WithCancel(context.Background())
	s.pushCancel = cancel
	s.pushTicker = time.NewTicker(time.Hour)
	defer s.pushTicker.Stop()

	started := make(chan struct{})
	var once sync.Once
	s.pushWG.Add(1)
	go func() {
		defer s.pushWG.Done()
		once.Do(func() { close(started) })
		// Stands in for an export to an address that never answers.
		select {
		case <-pushCtx.Done():
		case <-time.After(60 * time.Second):
			t.Error("the push was never aborted; shutdown would have waited for the export timeout")
		}
	}()
	<-started

	done := make(chan struct{})
	go func() {
		s.pushTicker.Stop()
		close(s.pushDone)
		if s.pushCancel != nil {
			s.pushCancel()
		}
		s.pushWG.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not return promptly: a push in flight still holds it")
	}
}
