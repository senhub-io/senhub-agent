package auto_update

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCheckForUpdateOnce_OperatorUpdateInProgressContactsNothing(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	au := &autoUpdate{
		configSource: newMockConfiguration("", `{"agent":{"version":"latest","registry_url":"`+srv.URL+`"}}`),
		logger:       createTestModuleLogger(),
		httpClient:   srv.Client(),
		acquireUpdateLock: func() (func(), error) {
			return nil, ErrUpdateInProgress
		},
	}

	if err := au.PeriodicalCheckForUpdate(); err != nil {
		t.Fatalf("PeriodicalCheckForUpdate: %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("expected no registry calls, got %d", got)
	}
}

func TestCheckForUpdateOnce_HoldsTheLockForTheCycle(t *testing.T) {
	var calls atomic.Int32
	var released atomic.Int32
	var lockHeldDuringCall atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if released.Load() != 0 {
			lockHeldDuringCall.Store(true)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	au := &autoUpdate{
		configSource: newMockConfiguration("", `{"agent":{"version":"latest","registry_url":"`+srv.URL+`"}}`),
		logger:       createTestModuleLogger(),
		httpClient:   srv.Client(),
		acquireUpdateLock: func() (func(), error) {
			return func() { released.Add(1) }, nil
		},
	}

	if err := au.PeriodicalCheckForUpdate(); err != nil {
		t.Fatalf("PeriodicalCheckForUpdate: %v", err)
	}
	if calls.Load() < 1 {
		t.Fatal("expected at least one registry call")
	}
	if released.Load() != 1 {
		t.Fatalf("expected release called exactly once, got %d", released.Load())
	}
	if lockHeldDuringCall.Load() {
		t.Fatal("lock was released before the registry call completed")
	}
}

func TestCheckForUpdateOnce_LockErrorStillChecks(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	au := &autoUpdate{
		configSource: newMockConfiguration("", `{"agent":{"version":"latest","registry_url":"`+srv.URL+`"}}`),
		logger:       createTestModuleLogger(),
		httpClient:   srv.Client(),
		acquireUpdateLock: func() (func(), error) {
			return nil, errors.New("disk")
		},
	}

	_ = au.PeriodicalCheckForUpdate()
	if calls.Load() < 1 {
		t.Fatal("expected the update check to proceed despite the lock error")
	}
}
