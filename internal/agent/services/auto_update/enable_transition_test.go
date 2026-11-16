package auto_update

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newCountingRegistry(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newTestAutoUpdate(cfg *mockConfiguration, client *http.Client) *autoUpdate {
	a := &autoUpdate{configSource: cfg, logger: createTestModuleLogger(), httpClient: client}
	a.acquireUpdateLock = func() (func(), error) { return func() {}, nil }
	return a
}

func TestOnConfigChange_EnablingWhileRunningChecksNow(t *testing.T) {
	srv, calls := newCountingRegistry(t)
	cfg := newMockConfiguration("", `{"agent":{"version":"latest","registry_url":"`+srv.URL+`","update_enabled":false}}`)
	a := newTestAutoUpdate(cfg, srv.Client())
	a.noteEnabled(false)

	data := cfg.GetConfiguration()
	data.Agent.UpdateEnabled = true
	cfg.SetConfiguration(data)
	a.onConfigChange("test")

	if got := calls.Load(); got < 1 {
		t.Fatalf("expected at least one registry call after enabling, got %d", got)
	}
}

func TestOnConfigChange_StaysDisabledContactsNothing(t *testing.T) {
	srv, calls := newCountingRegistry(t)
	cfg := newMockConfiguration("", `{"agent":{"version":"latest","registry_url":"`+srv.URL+`","update_enabled":false}}`)
	a := newTestAutoUpdate(cfg, srv.Client())
	a.noteEnabled(false)

	data := cfg.GetConfiguration()
	data.Agent.UpdateEnabled = false
	cfg.SetConfiguration(data)
	a.onConfigChange("test")

	if got := calls.Load(); got != 0 {
		t.Fatalf("expected zero registry calls while disabled, got %d", got)
	}
}

func TestNoteEnabled_WarnsWhenTurnedOnAndNotReplaceable(t *testing.T) {
	var probeCalls atomic.Int32
	orig := checkBinaryReplaceable
	checkBinaryReplaceable = func() string {
		probeCalls.Add(1)
		return "not replaceable"
	}
	t.Cleanup(func() { checkBinaryReplaceable = orig })

	a := &autoUpdate{logger: createTestModuleLogger()}
	a.noteEnabled(false)
	a.noteEnabled(true)
	a.noteEnabled(true)

	if got := probeCalls.Load(); got != 1 {
		t.Fatalf("expected exactly one replaceability probe on the false->true transition, got %d", got)
	}
}

func TestSetPaused_ResumeRunsHookOnce(t *testing.T) {
	ch := make(chan struct{}, 1)
	setResumeHook(func() { ch <- struct{}{} })
	t.Cleanup(func() {
		setResumeHook(nil)
		SetPaused(false)
	})

	SetPaused(true)
	SetPaused(false)
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("resume hook not called within 2s after SetPaused(false)")
	}

	SetPaused(false)
	select {
	case <-ch:
		t.Fatal("hook called again without a pause transition")
	case <-time.After(200 * time.Millisecond):
	}

	SetPaused(true)
	select {
	case <-ch:
		t.Fatal("hook called when pausing")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestResumeTriggersARegistryCheck(t *testing.T) {
	srv, calls := newCountingRegistry(t)
	cfg := newMockConfiguration("", `{"agent":{"version":"latest","registry_url":"`+srv.URL+`","update_enabled":true}}`)
	a := newTestAutoUpdate(cfg, srv.Client())
	done := make(chan struct{})
	setResumeHook(func() {
		a.checkAfterResume()
		close(done)
	})
	t.Cleanup(func() {
		setResumeHook(nil)
		SetPaused(false)
	})

	SetPaused(true)
	SetPaused(false)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("resume check did not finish within 5s")
	}
	if got := calls.Load(); got < 1 {
		t.Fatalf("expected at least one registry call after resume, got %d", got)
	}
}
