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

	snapshots  map[string]Snapshot
	listed     []string
	listErr    error
	inheritErr error

	appliedPaths []string
	inherited    []string
	inspected    []string
}

func (f *fakeBackend) inspect(path string) (Snapshot, error) {
	f.inspected = append(f.inspected, path)
	if f.inspectErr != nil {
		return Snapshot{}, f.inspectErr
	}
	if f.snapshots != nil {
		snap, ok := f.snapshots[path]
		if !ok {
			return Snapshot{Protected: true}, nil
		}
		return snap, nil
	}
	return f.snap, nil
}

func (f *fakeBackend) list(string) ([]string, error) { return f.listed, f.listErr }

func (f *fakeBackend) inherit(path string) error {
	f.inherited = append(f.inherited, path)
	return f.inheritErr
}

func (f *fakeBackend) apply(path string) error {
	f.applied++
	f.appliedPaths = append(f.appliedPaths, path)
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

func TestEnsureTreeErrors(t *testing.T) {
	failing := &fakeBackend{snap: Snapshot{}, applyErr: errors.New("denied")}
	if _, err := ensureTree(failing, "d", nopLogger()); !errors.Is(err, failing.applyErr) {
		t.Fatalf("apply failure not wrapped: %v", err)
	}

	broken := &fakeBackend{inspectErr: errors.New("boom")}
	if _, err := ensureTree(broken, "d", nopLogger()); !errors.Is(err, broken.inspectErr) {
		t.Fatalf("inspect failure not wrapped: %v", err)
	}
}

func TestEnsureTreeRepairsDescendant(t *testing.T) {
	children := []string{"agent.yaml", "logs"}
	b := &fakeBackend{
		snapshots: map[string]Snapshot{
			"d":          {Protected: true, Allowed: []ACE{{SID: "S-1-5-18"}}},
			"agent.yaml": {Protected: true, Allowed: []ACE{{SID: sidBuiltinUsers}}},
			"logs":       {Protected: true, Allowed: []ACE{{SID: "S-1-5-18"}}},
		},
		listed: children,
	}

	count, err := ensureTree(b, "d", nopLogger())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1+len(children) {
		t.Fatalf("count=%d want %d", count, 1+len(children))
	}
	if b.applied != 1 {
		t.Fatalf("applied=%d want 1", b.applied)
	}
	if len(b.appliedPaths) != 1 || b.appliedPaths[0] != "d" {
		t.Fatalf("apply paths=%v want [d]", b.appliedPaths)
	}
	if len(b.inherited) != len(children) {
		t.Fatalf("inherited=%v want %v", b.inherited, children)
	}
	for i := range children {
		if b.inherited[i] != children[i] {
			t.Fatalf("inherit order=%v want %v", b.inherited, children)
		}
	}
}

func TestEnsureTreeCleanTree(t *testing.T) {
	b := &fakeBackend{
		snapshots: map[string]Snapshot{
			"d": {Protected: true, Allowed: []ACE{{SID: "S-1-5-18"}}},
			"a": {Protected: true, Allowed: []ACE{{SID: "S-1-5-18"}}},
		},
		listed: []string{"a"},
	}

	count, err := ensureTree(b, "d", nopLogger())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || b.applied != 0 || len(b.inherited) != 0 {
		t.Fatalf("clean tree: count=%d applied=%d inherited=%v", count, b.applied, b.inherited)
	}
}

func TestEnsureTreeRootWeak(t *testing.T) {
	b := &fakeBackend{
		snapshots: map[string]Snapshot{
			"d": {Protected: false, Allowed: []ACE{{SID: "S-1-5-18"}}},
		},
		listed: []string{"a"},
	}

	count, err := ensureTree(b, "d", nopLogger())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count=%d want 2", count)
	}
	if b.applied != 1 {
		t.Fatalf("applied=%d want 1", b.applied)
	}
	if len(b.inherited) != 1 || b.inherited[0] != "a" {
		t.Fatalf("inherited=%v want [a]", b.inherited)
	}
}

func TestEnsureTreeDetectsBroadDescendant(t *testing.T) {
	for _, sid := range []string{sidEveryone, sidAuthenticatedUsers} {
		t.Run(sid, func(t *testing.T) {
			b := &fakeBackend{
				snapshots: map[string]Snapshot{
					"d":     {Protected: true, Allowed: []ACE{{SID: "S-1-5-18"}}},
					"child": {Protected: true, Allowed: []ACE{{SID: sid}}},
				},
				listed: []string{"child"},
			}

			count, err := ensureTree(b, "d", nopLogger())
			if err != nil {
				t.Fatal(err)
			}
			if count != 2 {
				t.Fatalf("count=%d want 2", count)
			}
			if b.applied != 1 {
				t.Fatalf("applied=%d want 1", b.applied)
			}
			if len(b.inherited) != 1 || b.inherited[0] != "child" {
				t.Fatalf("inherited=%v want [child]", b.inherited)
			}
		})
	}
}

func TestEnsureTreeListError(t *testing.T) {
	b := &fakeBackend{
		snapshots: map[string]Snapshot{
			"d": {Protected: true, Allowed: []ACE{{SID: "S-1-5-18"}}},
		},
		listErr: errors.New("list boom"),
	}

	_, err := ensureTree(b, "d", nopLogger())
	if !errors.Is(err, b.listErr) {
		t.Fatalf("error %v does not wrap listErr", err)
	}
	if b.applied != 0 || len(b.inherited) != 0 {
		t.Fatalf("list error should prevent apply/inherit")
	}
}

func TestEnsureTreeInheritError(t *testing.T) {
	b := &fakeBackend{
		snapshots: map[string]Snapshot{
			"d":     {Protected: true, Allowed: []ACE{{SID: "S-1-5-18"}}},
			"child": {Protected: true, Allowed: []ACE{{SID: sidBuiltinUsers}}},
		},
		listed:     []string{"child"},
		inheritErr: errors.New("inherit boom"),
	}

	_, err := ensureTree(b, "d", nopLogger())
	if !errors.Is(err, b.inheritErr) {
		t.Fatalf("error %v does not wrap inheritErr", err)
	}
	if b.applied != 1 {
		t.Fatalf("apply should run before inherit error")
	}
	if len(b.inherited) != 1 {
		t.Fatalf("inherit should have been attempted once")
	}
}
