package hostpoll

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveTakeWarmRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warm.json")

	type saved struct {
		Total uint64 `json:"total"`
		At    time.Time
	}
	in := saved{Total: 42, At: time.Now().UTC()}
	if err := SaveWarm(path, in); err != nil {
		t.Fatalf("SaveWarm: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file left behind: %v", err)
	}

	var out saved
	if err := TakeWarm(path, &out); err != nil {
		t.Fatalf("TakeWarm: %v", err)
	}
	if out.Total != in.Total {
		t.Errorf("Total = %d, want %d", out.Total, in.Total)
	}
	if !out.At.Equal(in.At) {
		t.Errorf("At = %v, want %v", out.At, in.At)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file not removed: %v", err)
	}
}

func TestTakeWarmMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	var v struct{}
	err := TakeWarm(path, &v)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestTakeWarmCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var v struct{}
	if err := TakeWarm(path, &v); err == nil {
		t.Fatal("expected error for corrupt file")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt file not removed: %v", statErr)
	}
}

func TestWarmUsable(t *testing.T) {
	old := BootIdentity
	defer func() { BootIdentity = old }()
	BootIdentity = func() (string, error) { return "boot-A", nil }

	now := time.Now()
	base := now.Add(-30 * time.Second)

	cases := []struct {
		name      string
		savedBoot string
		readAt    time.Time
		identity  func() (string, error)
		maxAge    time.Duration
		want      bool
	}{
		{"fresh same boot", "boot-A", base, nil, 60 * time.Second, true},
		{"too old", "boot-A", now.Add(-61 * time.Second), nil, 60 * time.Second, false},
		{"different boot", "boot-B", base, nil, 60 * time.Second, false},
		{"empty boot", "", base, nil, 60 * time.Second, false},
		{"identity error", "boot-A", base, func() (string, error) { return "", errors.New("boom") }, 60 * time.Second, false},
		{"future read", "boot-A", now.Add(10 * time.Second), nil, 60 * time.Second, false},
		{"zero readAt", "boot-A", time.Time{}, nil, 60 * time.Second, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.identity != nil {
				BootIdentity = tc.identity
				defer func() { BootIdentity = func() (string, error) { return "boot-A", nil } }()
			}
			got := WarmUsable(tc.savedBoot, tc.readAt, now, tc.maxAge)
			if got != tc.want {
				t.Errorf("WarmUsable = %v, want %v", got, tc.want)
			}
		})
	}
}

type stateAwareFake struct {
	fakeCollector
	path   string
	maxAge time.Duration
}

func (f *stateAwareFake) SetState(path string, maxAge time.Duration) {
	f.path = path
	f.maxAge = maxAge
}

func TestOnStartWiresState(t *testing.T) {
	t.Setenv("SENHUB_STATE_DIR", t.TempDir())
	fake := &stateAwareFake{}
	p := newTestProbe(t, map[string]interface{}{"interval": 60}, fake)
	p.SetName("my-probe")
	if err := p.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if filepath.Base(fake.path) != "my-probe.warm.json" {
		t.Errorf("path = %q, want suffix my-probe.warm.json", fake.path)
	}
	if fake.maxAge != 120*time.Second {
		t.Errorf("maxAge = %v, want 120s", fake.maxAge)
	}
}

func TestOnStartNonStateAware(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"interval": 60}, &fakeCollector{})
	if err := p.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
}
