package auto_update

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPeriodicalCheckForUpdate_SecondCycleIsSkippedWhileOneRuns(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{})
	var once sync.Once
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		once.Do(func() { close(entered) })
		<-release
		http.NotFound(w, r)
	}))
	t.Cleanup(func() {
		closeRelease()
		srv.Close()
	})

	au := &autoUpdate{
		configSource: newMockConfiguration("", `{"agent":{"update_enabled":true,"version":"latest","registry_url":"`+srv.URL+`"}}`),
		logger:       createTestModuleLogger(),
		httpClient:   srv.Client(),
	}

	done1 := make(chan error, 1)
	go func() {
		done1 <- au.PeriodicalCheckForUpdate()
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first cycle did not reach the server")
	}

	done2 := make(chan error, 1)
	go func() {
		done2 <- au.PeriodicalCheckForUpdate()
	}()
	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("second concurrent cycle returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second concurrent cycle blocked instead of being skipped")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected 1 request while first cycle runs, got %d", got)
	}

	closeRelease()
	select {
	case <-done1:
	case <-time.After(5 * time.Second):
		t.Fatal("first cycle did not finish after release")
	}

	if err := au.PeriodicalCheckForUpdate(); err != nil {
		t.Fatalf("third cycle returned error: %v", err)
	}
	if got := requests.Load(); got < 2 {
		t.Fatalf("expected lock to be released after first cycle, requests = %d", got)
	}
}

func TestPeriodicalCheckForUpdate_ReloadDuringACycleRunsAgain(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{})
	var once sync.Once
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		once.Do(func() { close(entered) })
		<-release
		http.NotFound(w, r)
	}))
	t.Cleanup(func() {
		closeRelease()
		srv.Close()
	})

	au := &autoUpdate{
		configSource: newMockConfiguration("", `{"agent":{"update_enabled":true,"version":"latest","registry_url":"`+srv.URL+`"}}`),
		logger:       createTestModuleLogger(),
		httpClient:   srv.Client(),
	}

	done1 := make(chan error, 1)
	go func() {
		done1 <- au.PeriodicalCheckForUpdate()
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first cycle did not reach the server")
	}

	done2 := make(chan error, 1)
	go func() {
		done2 <- au.PeriodicalCheckForUpdate()
	}()
	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("second concurrent cycle returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second concurrent cycle blocked instead of being skipped")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected 1 request while first cycle runs, got %d", got)
	}

	closeRelease()
	select {
	case <-done1:
	case <-time.After(5 * time.Second):
		t.Fatal("first cycle did not finish after release")
	}

	if got := requests.Load(); got != 2 {
		t.Fatalf("expected deferred cycle to run again before first call returned, requests = %d", got)
	}
}
