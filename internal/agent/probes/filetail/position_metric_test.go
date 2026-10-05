package filetail

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

// positionOf returns the value of a per-path position metric and whether
// the cycle carried it, checking the path attribute on the way.
func positionOf(t *testing.T, pts []data_store.DataPoint, name, path string) (float64, bool) {
	t.Helper()
	for _, dp := range pts {
		if dp.Name != name {
			continue
		}
		for _, tag := range dp.Tags {
			if tag.Key == "log.file.path" && tag.Value == path {
				return dp.Value, true
			}
		}
	}
	return 0, false
}

func collectPoints(t *testing.T, p *FileTailProbe) []data_store.DataPoint {
	t.Helper()
	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return pts
}

func TestFileTail_PositionMetricsPerPathCatchUpAfterAppend(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.log")
	b := filepath.Join(dir, "b.log")
	writeFile(t, a, backlog(10))
	writeFile(t, b, backlog(5))

	p := startFileTail(t, map[string]interface{}{
		"paths": []interface{}{a, b},
	})
	defer stopFileTail(t, p)
	awaitTailWatching(t, p, a)
	awaitTailWatching(t, p, b)

	pts := collectPoints(t, p)
	for _, path := range []string{a, b} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		off, okOff := positionOf(t, pts, "senhub.filetail.read_offset", path)
		size, okSize := positionOf(t, pts, "senhub.filetail.file_size", path)
		if !okOff || !okSize {
			t.Fatalf("%s: offset reported %v, size reported %v; points %+v", path, okOff, okSize, pts)
		}
		if size != float64(fi.Size()) {
			t.Errorf("%s: file_size = %v, want %d", path, size, fi.Size())
		}
		if off != size {
			t.Errorf("%s: a tail started at the end has offset %v, want the size %v", path, off, size)
		}
	}

	base := p.emitted.Load()
	appendLines(t, a, "2026-09-30 10:01:00 INFO appended one", "2026-09-30 10:01:01 INFO appended two")
	if got := waitEmitted(p, base+2, 5*time.Second); got < base+2 {
		t.Fatalf("appended lines were never read: emitted %d", got)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		pts = collectPoints(t, p)
		off, _ := positionOf(t, pts, "senhub.filetail.read_offset", a)
		size, _ := positionOf(t, pts, "senhub.filetail.file_size", a)
		if off == size && size > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("offset %v never caught up with size %v", off, size)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFileTail_FrozenTailShowsSizeAheadOfOffset(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "frozen.log")
	writeFile(t, file, backlog(10))

	p := newTestFileTailProbe(t)
	p.tailing = map[string]*tailState{file: {}}
	p.tailing[file].offset.Store(100)

	pts := collectPoints(t, p)
	off, okOff := positionOf(t, pts, "senhub.filetail.read_offset", file)
	size, okSize := positionOf(t, pts, "senhub.filetail.file_size", file)
	if !okOff || !okSize {
		t.Fatalf("offset reported %v, size reported %v", okOff, okSize)
	}
	if off != 100 {
		t.Errorf("read_offset = %v, want 100", off)
	}
	if size <= off {
		t.Errorf("a tail that does not read must show size > offset, got size %v offset %v", size, off)
	}
}

func TestFileTail_StatFailureEmitsOffsetOnly(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gone.log")

	p := newTestFileTailProbe(t)
	p.tailing = map[string]*tailState{file: {}}
	p.tailing[file].offset.Store(42)

	pts := collectPoints(t, p)
	if off, ok := positionOf(t, pts, "senhub.filetail.read_offset", file); !ok || off != 42 {
		t.Errorf("read_offset = %v (reported %v), want 42", off, ok)
	}
	if _, ok := positionOf(t, pts, "senhub.filetail.file_size", file); ok {
		t.Error("file_size must not be reported when the stat fails")
	}
}

func TestFileTail_PositionMetricsSurviveAnUnreadablePath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ok.log")
	writeFile(t, file, backlog(3))

	p := newTestFileTailProbe(t)
	p.tailing = map[string]*tailState{file: {}}
	p.issues = map[string]string{filepath.Join(dir, "denied.log"): "permission denied"}

	pts, err := p.Collect()
	if err == nil {
		t.Fatal("the unreadable path must still be reported as an error")
	}
	if _, ok := positionOf(t, pts, "senhub.filetail.file_size", file); !ok {
		t.Error("the healthy path lost its metrics because another path is unreadable")
	}
}

func TestFileTail_PositionGaugesCarryIdenticalTagSets(t *testing.T) {
	file := filepath.Join(t.TempDir(), "same.log")
	writeFile(t, file, backlog(4))

	p := newTestFileTailProbe(t)
	p.tailing = map[string]*tailState{file: {}}

	tagSet := func(name string) map[string]string {
		for _, dp := range collectPoints(t, p) {
			if dp.Name != name {
				continue
			}
			m := map[string]string{}
			for _, tag := range dp.Tags {
				m[tag.Key] = tag.Value
			}
			return m
		}
		t.Fatalf("%s not emitted", name)
		return nil
	}
	off, size := tagSet("senhub.filetail.read_offset"), tagSet("senhub.filetail.file_size")
	if len(off) != len(size) {
		t.Fatalf("tag sets differ: offset %v, size %v", off, size)
	}
	for k, v := range off {
		if size[k] != v {
			t.Errorf("tag %s: offset %q, size %q", k, v, size[k])
		}
	}
	if off["log.file.path"] != file {
		t.Errorf("log.file.path = %q, want %q", off["log.file.path"], file)
	}
}

func TestFileTail_AwaitedPathEmitsNoPosition(t *testing.T) {
	p := newTestFileTailProbe(t)
	p.awaiting = map[string]bool{"/var/log/not-yet.log": true}

	for _, dp := range collectPoints(t, p) {
		if dp.Name == "senhub.filetail.read_offset" || dp.Name == "senhub.filetail.file_size" {
			t.Errorf("unexpected %s for a path nobody tails", dp.Name)
		}
	}
}
