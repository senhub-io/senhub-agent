package kuberneteslogs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/probesdk/logs"
)

const testTS = "2026-10-13T10:00:00.000000001Z"

func criLogLine(ts, stream, tag, msg string) string {
	return fmt.Sprintf("%s %s %s %s", ts, stream, tag, msg)
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestProbe(t *testing.T, cfg map[string]interface{}) (*Probe, *[]logs.Record) {
	t.Helper()
	probe, err := NewKubernetesLogsProbe(cfg, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewKubernetesLogsProbe: %v", err)
	}
	p := probe.(*Probe)
	records := make([]logs.Record, 0)
	p.publish = func(r logs.Record) {
		records = append(records, r)
	}
	return p, &records
}

func assertRecordAttrs(t *testing.T, rec logs.Record, ns, pod, uid, container, node, stream, filePath string) {
	t.Helper()
	if rec.Attributes == nil {
		t.Fatalf("record attributes nil")
	}
	checks := map[string]string{
		"k8s.namespace.name": ns,
		"k8s.pod.name":       pod,
		"k8s.pod.uid":        uid,
		"k8s.container.name": container,
		"k8s.node.name":      node,
		"log.iostream":       stream,
		"log.file.path":      filePath,
	}
	for k, want := range checks {
		if got := rec.Attributes[k]; got != want {
			t.Fatalf("attribute %s = %q, want %q", k, got, want)
		}
	}
}

func TestKubernetesLogs_FirstScanFromEndThenAppend(t *testing.T) {
	t.Setenv("SENHUB_K8S_NODE_NAME", "node-a")
	dir := t.TempDir()
	bmDir := t.TempDir()
	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	writeLines(t, podPath,
		criLogLine(testTS, "stdout", "F", "existing one"),
		criLogLine(testTS, "stdout", "F", "existing two"),
	)

	cfg := map[string]interface{}{
		"root":          dir,
		"bookmark_path": filepath.Join(bmDir, "bm.json"),
	}
	p, records := newTestProbe(t, cfg)

	p.scanOnce()
	if len(*records) != 0 {
		t.Fatalf("first scan published %d records, want 0", len(*records))
	}

	appendLines(t, podPath, criLogLine(testTS, "stdout", "F", "new line"))
	p.scanOnce()

	got := *records
	if len(got) != 1 {
		t.Fatalf("after append published %d records, want 1", len(got))
	}
	rec := got[0]
	assertRecordAttrs(t, rec, "ns1", "pod1", "uid1", "cont1", "node-a", "stdout", podPath)
	if rec.Body != "new line" {
		t.Fatalf("body = %q, want %q", rec.Body, "new line")
	}
}

func TestKubernetesLogs_FromBeginningReadsExisting(t *testing.T) {
	t.Setenv("SENHUB_K8S_NODE_NAME", "node-a")
	dir := t.TempDir()
	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	writeLines(t, podPath,
		criLogLine(testTS, "stdout", "F", "first"),
		criLogLine(testTS, "stderr", "F", "second"),
	)

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()

	got := *records
	if len(got) != 2 {
		t.Fatalf("published %d records, want 2", len(got))
	}
	if got[0].Body != "first" || got[1].Body != "second" {
		t.Fatalf("bodies got %q, %q, want %q, %q", got[0].Body, got[1].Body, "first", "second")
	}
}

func TestKubernetesLogs_PodCreatedAfterFirstScan(t *testing.T) {
	t.Setenv("SENHUB_K8S_NODE_NAME", "node-a")
	dir := t.TempDir()
	cfg := map[string]interface{}{"root": dir}
	p, records := newTestProbe(t, cfg)

	p.scanOnce() // empty root
	if len(*records) != 0 {
		t.Fatalf("initial empty scan published %d records, want 0", len(*records))
	}

	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	writeLines(t, podPath, criLogLine(testTS, "stdout", "F", "late pod line"))
	p.scanOnce()

	got := *records
	if len(got) != 1 {
		t.Fatalf("after pod creation published %d records, want 1", len(got))
	}
	if got[0].Body != "late pod line" {
		t.Fatalf("body = %q, want %q", got[0].Body, "late pod line")
	}
}

func TestKubernetesLogs_PartialLineCompletedLater(t *testing.T) {
	dir := t.TempDir()
	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	dirPath := filepath.Dir(podPath)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(podPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(criLogLine(testTS, "stdout", "F", "partial")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()
	if len(*records) != 0 {
		t.Fatalf("partial line without newline published %d records, want 0", len(*records))
	}

	// Complete the line.
	f, err = os.OpenFile(podPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	p.scanOnce()
	got := *records
	if len(got) != 1 {
		t.Fatalf("after completing line published %d records, want 1", len(got))
	}
	if got[0].Body != "partial" {
		t.Fatalf("body = %q, want %q", got[0].Body, "partial")
	}
}

func TestKubernetesLogs_PartialChunksReassembled(t *testing.T) {
	dir := t.TempDir()
	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	writeLines(t, podPath,
		criLogLine(testTS, "stdout", "P", "part1"),
		criLogLine(testTS, "stdout", "P", "part2"),
		criLogLine(testTS, "stdout", "F", "part3"),
	)

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()

	got := *records
	if len(got) != 1 {
		t.Fatalf("published %d records, want 1", len(got))
	}
	if got[0].Body != "part1part2part3" {
		t.Fatalf("body = %q, want %q", got[0].Body, "part1part2part3")
	}
	if got[0].Attributes["log.iostream"] != "stdout" {
		t.Fatalf("stream = %q, want stdout", got[0].Attributes["log.iostream"])
	}
}

func TestKubernetesLogs_RotationPublishesAllLinesInOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rotation is followed by inode, which Windows does not expose; the probe is Linux-only")
	}
	t.Setenv("SENHUB_K8S_NODE_NAME", "node-rot")
	dir := t.TempDir()
	bmDir := t.TempDir()
	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	writeLines(t, podPath,
		criLogLine(testTS, "stdout", "F", "one"),
		criLogLine(testTS, "stdout", "F", "two"),
	)

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
		"bookmark_path":  filepath.Join(bmDir, "bm.json"),
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()
	if len(*records) != 2 {
		t.Fatalf("first scan published %d records, want 2", len(*records))
	}

	// Simulate Kubernetes rotation: rename current 0.log to rotated,
	// append more lines to the renamed file, create a fresh 0.log.
	rotatedPath := filepath.Join(filepath.Dir(podPath), "0.log.20261013-100000")
	if err := os.Rename(podPath, rotatedPath); err != nil {
		t.Fatal(err)
	}
	appendLines(t, rotatedPath, criLogLine(testTS, "stdout", "F", "three"))
	writeLines(t, podPath,
		criLogLine(testTS, "stdout", "F", "four"),
		criLogLine(testTS, "stdout", "F", "five"),
	)

	p.scanOnce()

	got := *records
	if len(got) != 5 {
		t.Fatalf("after rotation published %d records, want 5", len(got))
	}
	wantBodies := []string{"one", "two", "three", "four", "five"}
	for i, want := range wantBodies {
		if got[i].Body != want {
			t.Fatalf("record %d body = %q, want %q", i, got[i].Body, want)
		}
	}
}

func TestKubernetesLogs_TruncationReadsNewContentFromZero(t *testing.T) {
	dir := t.TempDir()
	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	writeLines(t, podPath,
		criLogLine(testTS, "stdout", "F", "long line one"),
		criLogLine(testTS, "stdout", "F", "long line two"),
	)

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()
	if len(*records) != 2 {
		t.Fatalf("first scan published %d records, want 2", len(*records))
	}

	// Rewrite shorter, simulating truncation.
	writeLines(t, podPath, criLogLine(testTS, "stdout", "F", "short"))

	p.scanOnce()

	got := *records
	if len(got) != 3 {
		t.Fatalf("after truncation total records %d, want 3", len(got))
	}
	if got[2].Body != "short" {
		t.Fatalf("last body = %q, want %q", got[2].Body, "short")
	}
}

func TestKubernetesLogs_ContainerRestartReadsNewLog(t *testing.T) {
	dir := t.TempDir()
	podDir := filepath.Join(dir, "ns1_pod1_uid1", "cont1")
	if err := os.MkdirAll(podDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, filepath.Join(podDir, "0.log"),
		criLogLine(testTS, "stdout", "F", "first restart old"),
	)

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()
	if len(*records) != 1 {
		t.Fatalf("first scan published %d records, want 1", len(*records))
	}

	// New container restart log file.
	writeLines(t, filepath.Join(podDir, "1.log"),
		criLogLine(testTS, "stdout", "F", "second restart new"),
	)

	p.scanOnce()

	got := *records
	if len(got) != 2 {
		t.Fatalf("after restart published %d records, want 2", len(got))
	}
	if got[1].Body != "second restart new" {
		t.Fatalf("second record body = %q, want %q", got[1].Body, "second restart new")
	}
}

func TestKubernetesLogs_PodRemovalNoPanic(t *testing.T) {
	dir := t.TempDir()
	podDir1 := filepath.Join(dir, "ns1_pod1_uid1", "cont1")
	podDir2 := filepath.Join(dir, "ns2_pod2_uid2", "cont2")
	if err := os.MkdirAll(podDir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(podDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, filepath.Join(podDir1, "0.log"), criLogLine(testTS, "stdout", "F", "pod1 line"))
	writeLines(t, filepath.Join(podDir2, "0.log"), criLogLine(testTS, "stdout", "F", "pod2 line"))

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()
	if len(*records) != 2 {
		t.Fatalf("first scan published %d records, want 2", len(*records))
	}

	// Remove one pod directory; should not panic and metrics should drop.
	if err := os.RemoveAll(podDir1); err != nil {
		t.Fatal(err)
	}
	p.scanOnce()

	// Records already emitted should remain unchanged.
	if len(*records) != 2 {
		t.Fatalf("after removal records count = %d, want 2", len(*records))
	}
	if got := p.filesFollowed.Load(); got != 1 {
		t.Fatalf("files_followed = %d, want 1", got)
	}
	if got := p.podsFollowed.Load(); got != 1 {
		t.Fatalf("pods_followed = %d, want 1", got)
	}
}

func TestKubernetesLogs_OwnPodExcluded(t *testing.T) {
	t.Setenv("SENHUB_K8S_POD_NAMESPACE", "own-ns")
	t.Setenv("SENHUB_K8S_POD_NAME", "own-pod")
	t.Setenv("SENHUB_K8S_POD_UID", "own-uid")
	t.Setenv("SENHUB_K8S_NODE_NAME", "node-own")
	dir := t.TempDir()
	ownDir := filepath.Join(dir, "own-ns_own-pod_own-uid", "cont")
	otherDir := filepath.Join(dir, "other-ns_other-pod_other-uid", "cont")
	if err := os.MkdirAll(ownDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, filepath.Join(ownDir, "0.log"), criLogLine(testTS, "stdout", "F", "own line"))
	writeLines(t, filepath.Join(otherDir, "0.log"), criLogLine(testTS, "stdout", "F", "other line"))

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()

	got := *records
	if len(got) != 1 {
		t.Fatalf("published %d records, want 1 (own pod should be excluded)", len(got))
	}
	if got[0].Body != "other line" {
		t.Fatalf("body = %q, want %q", got[0].Body, "other line")
	}
}

func TestKubernetesLogs_ExcludeNamespaces(t *testing.T) {
	dir := t.TempDir()
	nsEx := filepath.Join(dir, "ns-excluded_pod1_uid1", "cont")
	nsKeep := filepath.Join(dir, "ns-keep_pod1_uid1", "cont")
	if err := os.MkdirAll(nsEx, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nsKeep, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, filepath.Join(nsEx, "0.log"), criLogLine(testTS, "stdout", "F", "excluded"))
	writeLines(t, filepath.Join(nsKeep, "0.log"), criLogLine(testTS, "stdout", "F", "kept"))

	cfg := map[string]interface{}{
		"root":               dir,
		"from_beginning":     true,
		"exclude_namespaces": []interface{}{"ns-excluded"},
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()

	got := *records
	if len(got) != 1 {
		t.Fatalf("published %d records, want 1", len(got))
	}
	if got[0].Body != "kept" {
		t.Fatalf("body = %q, want %q", got[0].Body, "kept")
	}
}

func TestKubernetesLogs_BookmarkResume(t *testing.T) {
	t.Setenv("SENHUB_K8S_NODE_NAME", "node-resume")
	dir := t.TempDir()
	bmDir := t.TempDir()
	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	writeLines(t, podPath,
		criLogLine(testTS, "stdout", "F", "line1"),
		criLogLine(testTS, "stdout", "F", "line2"),
		criLogLine(testTS, "stdout", "F", "line3"),
	)

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
		"bookmark_path":  filepath.Join(bmDir, "bm.json"),
	}

	// Probe A reads the first three lines and shuts down, flushing bookmarks.
	pA, recA := newTestProbe(t, cfg)
	quitA := make(chan struct{})
	if err := pA.OnStart(quitA); err != nil {
		t.Fatalf("probe A OnStart: %v", err)
	}
	if len(*recA) != 3 {
		t.Fatalf("probe A published %d records, want 3", len(*recA))
	}
	ctxA, cancelA := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelA()
	if err := pA.OnShutdown(ctxA); err != nil {
		t.Fatalf("probe A OnShutdown: %v", err)
	}

	// Append two more lines after shutdown.
	appendLines(t, podPath,
		criLogLine(testTS, "stdout", "F", "line4"),
		criLogLine(testTS, "stdout", "F", "line5"),
	)

	// Probe B must resume from the bookmark and publish only the two new lines.
	pB, recB := newTestProbe(t, cfg)
	quitB := make(chan struct{})
	if err := pB.OnStart(quitB); err != nil {
		t.Fatalf("probe B OnStart: %v", err)
	}
	got := *recB
	if len(got) != 2 {
		t.Fatalf("probe B published %d records, want 2", len(got))
	}
	if got[0].Body != "line4" || got[1].Body != "line5" {
		t.Fatalf("probe B bodies = %q, %q, want %q, %q", got[0].Body, got[1].Body, "line4", "line5")
	}
	ctxB, cancelB := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelB()
	if err := pB.OnShutdown(ctxB); err != nil {
		t.Fatalf("probe B OnShutdown: %v", err)
	}
}

func TestKubernetesLogs_CollectMetrics(t *testing.T) {
	t.Setenv("SENHUB_K8S_NODE_NAME", "node-metrics")
	dir := t.TempDir()
	podDir := filepath.Join(dir, "ns1_pod1_uid1", "cont1")
	if err := os.MkdirAll(podDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, filepath.Join(podDir, "0.log"), criLogLine(testTS, "stdout", "F", "metric line"))

	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
	}
	p, _ := newTestProbe(t, cfg)
	p.scanOnce()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	expected := map[string]float64{
		"senhub.kubernetes_logs.files_followed":    1,
		"senhub.kubernetes_logs.pods_followed":     1,
		"senhub.kubernetes_logs.records_emitted":   1,
		"senhub.kubernetes_logs.records_truncated": 0,
		"senhub.kubernetes_logs.read_errors":       0,
	}
	found := make(map[string]float64, len(points))
	for _, pt := range points {
		found[pt.Name] = pt.Value
	}
	for name, want := range expected {
		got, ok := found[name]
		if !ok {
			t.Fatalf("missing metric %s", name)
		}
		if got != want {
			t.Fatalf("metric %s = %v, want %v", name, got, want)
		}
	}
}

func TestKubernetesLogs_MultilineFoldsPerStream(t *testing.T) {
	dir := t.TempDir()
	podPath := filepath.Join(dir, "ns1_pod1_uid1", "cont1", "0.log")
	writeLines(t, podPath,
		criLogLine(testTS, "stdout", "F", "2026-10-13 10:00:00 ERROR boom"),
		criLogLine(testTS, "stderr", "F", "2026-10-13 10:00:00 WARN other stream"),
		criLogLine(testTS, "stdout", "F", "\tat com.example.A.run(A.java:1)"),
		criLogLine(testTS, "stdout", "F", "\tat com.example.B.run(B.java:2)"),
		criLogLine(testTS, "stdout", "F", "2026-10-13 10:00:01 INFO next"),
	)
	cfg := map[string]interface{}{
		"root":           dir,
		"from_beginning": true,
		"bookmark_path":  filepath.Join(t.TempDir(), "bm.json"),
		"multiline":      map[string]interface{}{"pattern": `^\d{4}-\d{2}-\d{2}`},
	}
	p, records := newTestProbe(t, cfg)
	p.scanOnce()

	var bodies []string
	for _, r := range *records {
		bodies = append(bodies, r.Body)
	}
	want := []string{
		"2026-10-13 10:00:00 ERROR boom\n\tat com.example.A.run(A.java:1)\n\tat com.example.B.run(B.java:2)",
	}
	if len(bodies) != 1 || bodies[0] != want[0] {
		t.Fatalf("after first cycle bodies = %q, want %q", bodies, want)
	}

	// No new byte: the cycle is idle and the records still being folded
	// (one per stream) are flushed.
	p.scanOnce()
	if len(*records) != 3 {
		t.Fatalf("after idle cycle got %d records, want 3", len(*records))
	}
	got := map[string]string{}
	for _, r := range (*records)[1:] {
		got[r.Attributes["log.iostream"]] = r.Body
	}
	if got["stdout"] != "2026-10-13 10:00:01 INFO next" || got["stderr"] != "2026-10-13 10:00:00 WARN other stream" {
		t.Fatalf("flushed records = %q", got)
	}
}
