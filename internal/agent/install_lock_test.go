package agent

import (
	"errors"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/auto_update"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

func TestTakeInstallLock_Busy(t *testing.T) {
	release, err := takeInstallLock(func() (func(), error) {
		return nil, auto_update.ErrUpdateInProgress
	}, testLogger(t))
	if !errors.Is(err, auto_update.ErrUpdateInProgress) {
		t.Fatalf("expected ErrUpdateInProgress, got %v", err)
	}
	if release != nil {
		t.Fatal("expected nil release on busy lock")
	}
}

func TestTakeInstallLock_OtherError(t *testing.T) {
	release, err := takeInstallLock(func() (func(), error) {
		return nil, errors.New("disk")
	}, testLogger(t))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if release == nil {
		t.Fatal("expected a callable release")
	}
	release()
}

func TestTakeInstallLock_Success(t *testing.T) {
	calls := 0
	release, err := takeInstallLock(func() (func(), error) {
		return func() { calls++ }, nil
	}, testLogger(t))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if release == nil {
		t.Fatal("expected the release from acquire")
	}
	release()
	if calls != 1 {
		t.Fatalf("expected acquire's release to be used, calls=%d", calls)
	}
}
