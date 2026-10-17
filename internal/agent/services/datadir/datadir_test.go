package datadir

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
)

type fakeBackend struct {
	snap       Snapshot
	inspectErr error
	applyErr   error
	applied    int
}

func (f *fakeBackend) inspect(string) (Snapshot, error) { return f.snap, f.inspectErr }
func (f *fakeBackend) apply(string) error {
	f.applied++
	return f.applyErr
}

func nopLogger() Logger {
	l := zerolog.Nop()
	return &l
}

func TestNeedsRepair(t *testing.T) {
	const system, admins = "S-1-5-18", "S-1-5-32-544"
	cases := []struct {
		name string
		snap Snapshot
		want bool
	}{
		{"protected system+admins", Snapshot{true, []ACE{{system, 0x1f01ff}, {admins, 0x1f01ff}}}, false},
		{"stock inherited", Snapshot{false, []ACE{{system, 0}, {admins, 0}, {"S-1-5-32-545", 0}}}, true},
		{"protected but users", Snapshot{true, []ACE{{system, 0}, {"S-1-5-32-545", 0x1200a9}}}, true},
		{"protected but everyone", Snapshot{true, []ACE{{"S-1-1-0", 0}}}, true},
		{"protected but authenticated users", Snapshot{true, []ACE{{"S-1-5-11", 0}}}, true},
		{"unprotected admins only", Snapshot{false, []ACE{{system, 0}}}, true},
	}
	for _, c := range cases {
		if got, _ := NeedsRepair(c.snap); got != c.want {
			t.Errorf("%s: NeedsRepair = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEnsure(t *testing.T) {
	good := &fakeBackend{snap: Snapshot{Protected: true, Allowed: []ACE{{SID: "S-1-5-18"}}}}
	if changed, err := ensure(good, "d", nopLogger()); err != nil || changed || good.applied != 0 {
		t.Fatalf("compliant dir: changed=%v err=%v applied=%d", changed, err, good.applied)
	}

	weak := &fakeBackend{snap: Snapshot{Allowed: []ACE{{SID: "S-1-5-32-545"}}}}
	if changed, err := ensure(weak, "d", nopLogger()); err != nil || !changed || weak.applied != 1 {
		t.Fatalf("weak dir: changed=%v err=%v applied=%d", changed, err, weak.applied)
	}

	failing := &fakeBackend{snap: Snapshot{}, applyErr: errors.New("denied")}
	if _, err := ensure(failing, "d", nopLogger()); !errors.Is(err, failing.applyErr) {
		t.Fatalf("apply failure not wrapped: %v", err)
	}

	broken := &fakeBackend{inspectErr: errors.New("boom")}
	if _, err := ensure(broken, "d", nopLogger()); !errors.Is(err, broken.inspectErr) {
		t.Fatalf("inspect failure not wrapped: %v", err)
	}
}
