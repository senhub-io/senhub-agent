package podlogs

import (
	"testing"
	"testing/fstest"
	"time"
)

func TestScanPods(t *testing.T) {
	t0 := time.Date(2026, 10, 13, 10, 15, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)

	fsys := fstest.MapFS{
		"var/log/pods/ns1_pod1_uid1/c1/0.log":                    {Data: []byte{}, ModTime: t1},
		"var/log/pods/ns1_pod1_uid1/c1/0.log.20261013-101500":    {Data: []byte{}, ModTime: t0},
		"var/log/pods/ns1_pod1_uid1/c1/0.log.20261013-101500.gz": {Data: []byte{}, ModTime: t0},
		"var/log/pods/ns1_pod1_uid1/c2/0.log":                    {Data: []byte{}, ModTime: t2},
		"var/log/pods/ns1_pod2_uid2/c1/0.log":                    {Data: []byte{}, ModTime: t1},
		"var/log/pods/ns1_pod2_uid2/c1/1.log":                    {Data: []byte{}, ModTime: t2},
		"var/log/pods/ns2_pod3_uid3/c1/0.log":                    {Data: []byte{}, ModTime: t1},
		"var/log/pods/ns1_self-pod_self-uid/c1/0.log":            {Data: []byte{}, ModTime: t1},
	}

	filter := Filter{IncludeNamespaces: []string{"ns1"}}
	self := PodRef{Namespace: "ns1", Pod: "self-pod", UID: "self-uid"}

	scan, err := ScanPods(fsys, "var/log/pods", filter, self)
	if err != nil {
		t.Fatal(err)
	}

	wantFiles := []string{
		"var/log/pods/ns1_pod1_uid1/c1/0.log",
		"var/log/pods/ns1_pod1_uid1/c2/0.log",
		"var/log/pods/ns1_pod2_uid2/c1/1.log",
	}
	if len(scan.Files) != len(wantFiles) {
		t.Fatalf("got %d files, want %d", len(scan.Files), len(wantFiles))
	}
	for i, f := range scan.Files {
		if f.Path != wantFiles[i] {
			t.Errorf("Files[%d] = %q, want %q", i, f.Path, wantFiles[i])
		}
	}

	wantRotated := []string{
		"var/log/pods/ns1_pod1_uid1/c1/0.log.20261013-101500",
		"var/log/pods/ns1_pod2_uid2/c1/0.log",
	}
	if len(scan.Rotated) != len(wantRotated) {
		t.Fatalf("got %d rotated, want %d", len(scan.Rotated), len(wantRotated))
	}
	for i, rf := range scan.Rotated {
		if rf.Path != wantRotated[i] {
			t.Errorf("Rotated[%d] = %q, want %q", i, rf.Path, wantRotated[i])
		}
	}
}

func makeFileState(path, ns, pod, uid, container string, restart int, size int64, inode uint64, mod time.Time) FileState {
	return FileState{
		Path: path,
		Ref: PodRef{
			Namespace:    ns,
			Pod:          pod,
			UID:          uid,
			Container:    container,
			RestartCount: restart,
			Rotated:      false,
		},
		Size:    size,
		ModTime: mod,
		Inode:   inode,
	}
}

func TestDiffAdded(t *testing.T) {
	f := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 100, 1, time.Now())
	changes := Diff(Scan{}, Scan{Files: []FileState{f}})
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if changes[0].Kind != Added || changes[0].After.Path != f.Path {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDiffRemoved(t *testing.T) {
	f := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 100, 1, time.Now())
	changes := Diff(Scan{Files: []FileState{f}}, Scan{})
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if changes[0].Kind != Removed || changes[0].Before.Path != f.Path {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDiffTruncated(t *testing.T) {
	t0 := time.Now()
	prev := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 200, 123, t0)
	cur := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 100, 123, t0.Add(time.Minute))
	changes := Diff(Scan{Files: []FileState{prev}}, Scan{Files: []FileState{cur}})
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if changes[0].Kind != Truncated || changes[0].Before.Size != 200 || changes[0].After.Size != 100 {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDiffRotatedByInode(t *testing.T) {
	t0 := time.Now()
	prev := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 200, 123, t0)
	cur := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 50, 456, t0.Add(time.Minute))
	rotated := FileState{
		Path: "ns_pod_uid/c/0.log.20261013-101500",
		Ref:  PodRef{Namespace: "ns", Pod: "pod", UID: "uid", Container: "c", RestartCount: 0, Rotated: true},
		Size: 200, ModTime: t0.Add(30 * time.Second), Inode: 789,
	}
	changes := Diff(Scan{Files: []FileState{prev}}, Scan{Files: []FileState{cur}, Rotated: []FileState{rotated}})
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if changes[0].Kind != Rotated || changes[0].Before.Inode != 123 || changes[0].After.Inode != 456 {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDiffRotatedBySize(t *testing.T) {
	t0 := time.Now()
	prev := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 200, 0, t0)
	cur := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 30, 0, t0.Add(time.Minute))
	rotated := FileState{
		Path: "ns_pod_uid/c/0.log.20261013-101500",
		Ref:  PodRef{Namespace: "ns", Pod: "pod", UID: "uid", Container: "c", RestartCount: 0, Rotated: true},
		Size: 200, ModTime: t0.Add(30 * time.Second), Inode: 0,
	}
	changes := Diff(Scan{Files: []FileState{prev}}, Scan{Files: []FileState{cur}, Rotated: []FileState{rotated}})
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if changes[0].Kind != Rotated || changes[0].Before.Size != 200 || changes[0].After.Size != 30 {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDiffRestartAdded(t *testing.T) {
	t0 := time.Now()
	oldPath := "ns_pod_uid/c/0.log"
	newPath := "ns_pod_uid/c/1.log"
	oldCurrent := makeFileState(oldPath, "ns", "pod", "uid", "c", 0, 200, 1, t0)
	newCurrent := makeFileState(newPath, "ns", "pod", "uid", "c", 1, 0, 2, t0.Add(time.Hour))
	rotatedOld := FileState{
		Path: oldPath,
		Ref:  PodRef{Namespace: "ns", Pod: "pod", UID: "uid", Container: "c", RestartCount: 0, Rotated: false},
		Size: 200, ModTime: t0, Inode: 1,
	}
	changes := Diff(Scan{Files: []FileState{oldCurrent}}, Scan{Files: []FileState{newCurrent}, Rotated: []FileState{rotatedOld}})
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if changes[0].Kind != Added || changes[0].After.Path != newPath {
		t.Errorf("unexpected change: %+v", changes[0])
	}
}

func TestDiffNoChange(t *testing.T) {
	t0 := time.Now()
	f := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 100, 123, t0)
	changes := Diff(Scan{Files: []FileState{f}}, Scan{Files: []FileState{f}})
	if len(changes) != 0 {
		t.Fatalf("got %d changes, want 0", len(changes))
	}
}

func TestDiffRotatedNewlyAppeared(t *testing.T) {
	t0 := time.Now()
	prev := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 200, 123, t0)
	cur := makeFileState("ns_pod_uid/c/0.log", "ns", "pod", "uid", "c", 0, 80, 123, t0.Add(time.Minute))
	// A rotated file with same size already existed before the truncation.
	prevRotated := FileState{
		Path: "ns_pod_uid/c/0.log.20261013-101500",
		Ref:  PodRef{Namespace: "ns", Pod: "pod", UID: "uid", Container: "c", RestartCount: 0, Rotated: true},
		Size: 200, ModTime: t0.Add(-time.Hour), Inode: 999,
	}
	changes := Diff(
		Scan{Files: []FileState{prev}, Rotated: []FileState{prevRotated}},
		Scan{Files: []FileState{cur}, Rotated: []FileState{prevRotated}},
	)
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if changes[0].Kind != Truncated {
		t.Errorf("unexpected change kind: %s", changes[0].Kind)
	}
}
