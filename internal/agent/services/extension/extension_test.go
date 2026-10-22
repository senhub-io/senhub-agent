package extension

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type nop struct{}

func (nop) Start(context.Context) error    { return nil }
func (nop) Shutdown(context.Context) error { return nil }

func newNop(Host) (Service, error) { return nop{}, nil }

func TestNothingRegisteredIsInert(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	if got := Registered(); len(got) != 0 {
		t.Errorf("Registered = %v, want none", got)
	}
	if got := ConfigBlocks(); len(got) != 0 {
		t.Errorf("ConfigBlocks = %v, want none", got)
	}
	if Statuses() != nil {
		t.Error("Statuses must be nil when nothing was published")
	}
}

func TestInvalidRegistrationsAreLeftOutAndExplained(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	Register(Extension{Name: "a", ConfigBlock: "blk", New: newNop})
	Register(Extension{Name: "", New: newNop})
	Register(Extension{Name: "b"})
	Register(Extension{Name: "a", New: newNop})
	Register(Extension{Name: "c", ConfigBlock: "blk", New: newNop})
	Register(Extension{Name: "d", New: newNop})

	got := Registered()
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "d" {
		t.Fatalf("Registered = %+v, want a then d", got)
	}
	blocks := ConfigBlocks()
	if len(blocks) != 1 || !blocks["blk"] {
		t.Errorf("ConfigBlocks = %v, want only blk", blocks)
	}
	problems := strings.Join(Problems(), "\n")
	for _, want := range []string{"no name", "b has no constructor", "a is registered twice", "c claims block blk"} {
		if !strings.Contains(problems, want) {
			t.Errorf("Problems lacks %q:\n%s", want, problems)
		}
	}
}

func TestStatusesAreCopies(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	in := map[string]any{"connected": true}
	SetStatus("a", in)
	in["connected"] = false
	got := Statuses()
	if got["a"]["connected"] != true {
		t.Errorf("a published status changed with the caller's map: %v", got)
	}
	got["a"]["connected"] = false
	if Statuses()["a"]["connected"] != true {
		t.Error("a returned status must not alias the stored one")
	}
	SetStatus("a", nil)
	if Statuses() != nil {
		t.Error("a nil status must remove the entry")
	}
}

func TestConcurrentUse(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			Register(Extension{Name: "x", New: newNop})
			SetStatus("x", map[string]any{"n": 1})
		}()
		go func() {
			defer wg.Done()
			_ = Registered()
			_ = Statuses()
		}()
	}
	wg.Wait()
}
