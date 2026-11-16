//go:build windows

package auto_update

import (
	"errors"
	"testing"
)

func TestAcquireUpdateLockExcludesASecondHolder(t *testing.T) {
	dir := t.TempDir()

	r1, err := AcquireUpdateLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err = AcquireUpdateLock(dir); !errors.Is(err, ErrUpdateInProgress) {
		t.Fatalf("second acquire: expected ErrUpdateInProgress, got %v", err)
	}

	r1()

	r3, err := AcquireUpdateLock(dir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	r3()

	r1() // must not panic
}
