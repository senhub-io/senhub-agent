package event

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/services/agentstate"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
	eventtypes "senhub-agent.go/internal/agent/types/event"
)

// TestMain keeps every strategy these tests start away from the real
// agent state directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "event-state-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("SENHUB_STATE_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// shutdownMargin is what a Shutdown test allows past the context deadline,
// on top of the time the backlog spent writing files. After the deadline
// Shutdown cancels the send, takes a mutex and writes; the writes are the
// only unbounded part, since the events must reach the disk to be kept and
// a saturated disk stalled a single small write for 4.7 s in a measured
// run. The rest is goroutine scheduling, for which one second is ample.
const shutdownMargin = time.Second

// overrun is how far a stop went past its deadline, disk writes excluded.
func overrun(elapsed, deadline time.Duration, b *eventBacklog) time.Duration {
	return elapsed - deadline - time.Duration(b.writeNanos.Load())
}

// ackIntake is a fake intake that records the messages of every batch it
// acknowledges, and checks at request time that the batch is still on disk.
type ackIntake struct {
	mu       sync.Mutex
	status   int
	dir      string
	received []string
	// onDiskAtRequest is the number of backlog files seen while the
	// request was in flight.
	onDiskAtRequest []int
}

func (a *ackIntake) setStatus(status int) {
	a.mu.Lock()
	a.status = status
	a.mu.Unlock()
}

func (a *ackIntake) Get(string) (*http.Response, error)       { return nil, fmt.Errorf("unused") }
func (a *ackIntake) Post(string, any) (*http.Response, error) { return nil, fmt.Errorf("unused") }

func (a *ackIntake) PostStream(_ string, body string) (*http.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onDiskAtRequest = append(a.onDiskAtRequest, countBacklogFiles(a.dir))
	if a.status == http.StatusOK || a.status == http.StatusAccepted {
		var events []map[string]any
		if err := json.Unmarshal([]byte(body), &events); err != nil {
			return nil, err
		}
		for _, e := range events {
			a.received = append(a.received, e["message"].(string))
		}
	}
	return &http.Response{StatusCode: a.status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func countBacklogFiles(dir string) int {
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

func newDurableStrategy(t *testing.T, dir string, extra configuration.StorageConfigParams) *EventSyncStrategy {
	t.Helper()
	params := configuration.StorageConfigParams{
		"server_url":   "http://intake.invalid",
		"backlog_path": dir,
	}
	for k, v := range extra {
		params[k] = v
	}
	s, err := NewEventSyncStrategy(stubAgentConfig{}, params, testBaseLogger())
	if err != nil {
		t.Fatalf("NewEventSyncStrategy: %v", err)
	}
	s.openBacklog()
	s.syncTriggerSize = 1 << 30
	s.syncTriggerBytes = 1 << 30
	s.retryAttempts = 1
	s.retryDelay = 0
	s.firstRetryDelay = 0
	s.firstRetryJitter = 0
	return s
}

// enqueueNamed queues events whose message is prefix+index.
func enqueueNamed(s *EventSyncStrategy, prefix string, from, n int) {
	for i := from; i < from+n; i++ {
		s.enqueue(eventtypes.EventDataPoint{
			"timestamp": time.Unix(1_700_000_000+int64(i), 0),
			"host":      "backlog-test-host",
			"severity":  "info",
			"message":   fmt.Sprintf("%s%03d", prefix, i),
		})
	}
}

func names(prefix string, from, n int) []string {
	out := make([]string, 0, n)
	for i := from; i < from+n; i++ {
		out = append(out, fmt.Sprintf("%s%03d", prefix, i))
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBacklogOutageThenRecoveryReplaysInOrder(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"server error", http.StatusInternalServerError},
		{"unavailable", http.StatusServiceUnavailable},
		{"too many requests", http.StatusTooManyRequests},
		{"request timeout", http.StatusRequestTimeout},
		{"unauthorized", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agentstate.ResetPushBufferDroppedForTest()
			dir := t.TempDir()
			intake := &ackIntake{status: tc.status, dir: dir}
			s := newDurableStrategy(t, dir, nil)
			s.server = intake

			for cycle := 0; cycle < 4; cycle++ {
				enqueueNamed(s, "m", cycle*3, 3)
				if err := s.doSync(context.Background()); err == nil {
					t.Fatalf("cycle %d: doSync must report the failed delivery", cycle)
				}
			}
			if got := countBacklogFiles(dir); got != 4 {
				t.Fatalf("backlog files = %d, want 4 (one per failed cycle)", got)
			}
			if len(s.buffer) != 0 {
				t.Fatalf("buffer holds %d events during the outage, want 0 (parked on disk)", len(s.buffer))
			}
			if n, _ := s.backlog.pending(); n != 12 {
				t.Fatalf("pending events = %d, want 12", n)
			}

			intake.setStatus(http.StatusAccepted)
			enqueueNamed(s, "m", 12, 2)
			if err := s.doSync(context.Background()); err != nil {
				t.Fatalf("doSync after recovery: %v", err)
			}
			if got := intake.received; !sameStrings(got, names("m", 0, 14)) {
				t.Fatalf("replay order = %v, want m000..m013 in order", got)
			}
			if got := countBacklogFiles(dir); got != 0 {
				t.Fatalf("backlog files after ack = %d, want 0", got)
			}
			if d := agentstate.GetPushBufferDropped()["event"]; d != 0 {
				t.Fatalf("dropped = %d, want 0", d)
			}
		})
	}
}

// TestBacklogFileDeletedOnlyAfterAck checks, from inside the intake, that
// the file still exists while its replay is in flight.
func TestBacklogFileDeletedOnlyAfterAck(t *testing.T) {
	dir := t.TempDir()
	intake := &ackIntake{status: http.StatusServiceUnavailable, dir: dir}
	s := newDurableStrategy(t, dir, nil)
	s.server = intake

	enqueueNamed(s, "m", 0, 3)
	_ = s.doSync(context.Background())
	intake.mu.Lock()
	intake.onDiskAtRequest = nil
	intake.mu.Unlock()

	intake.setStatus(http.StatusAccepted)
	if err := s.doSync(context.Background()); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if len(intake.onDiskAtRequest) != 1 || intake.onDiskAtRequest[0] != 1 {
		t.Fatalf("files on disk seen during the replay request = %v, want [1]", intake.onDiskAtRequest)
	}
	if countBacklogFiles(dir) != 0 {
		t.Fatal("file not removed after the acknowledgement")
	}

	// A refused replay keeps the file.
	intake.setStatus(http.StatusBadGateway)
	enqueueNamed(s, "n", 0, 2)
	_ = s.doSync(context.Background())
	if countBacklogFiles(dir) != 1 {
		t.Fatal("a batch the intake did not acknowledge must stay on disk")
	}
}

func TestBacklogSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	down := &ackIntake{status: http.StatusServiceUnavailable, dir: dir}
	first := newDurableStrategy(t, dir, nil)
	first.server = down
	enqueueNamed(first, "m", 0, 5)
	_ = first.doSync(context.Background())
	enqueueNamed(first, "m", 5, 5)
	_ = first.doSync(context.Background())

	// New instance, same directory: the process restarted mid-outage.
	up := &ackIntake{status: http.StatusAccepted, dir: dir}
	second := newDurableStrategy(t, dir, nil)
	second.server = up
	if n, _ := second.backlog.pending(); n != 10 {
		t.Fatalf("restarted strategy sees %d pending events, want 10", n)
	}
	enqueueNamed(second, "m", 10, 1)
	if err := second.doSync(context.Background()); err != nil {
		t.Fatalf("doSync after restart: %v", err)
	}
	if !sameStrings(up.received, names("m", 0, 11)) {
		t.Fatalf("replayed %v, want m000..m010 in order", up.received)
	}
	if countBacklogFiles(dir) != 0 {
		t.Fatal("backlog not emptied after replay")
	}
}

func TestBacklogKeepsEventIDsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	var ids [2][]string
	capture := func(i int) *captureIntake { return &captureIntake{ids: &ids[i]} }

	first := newDurableStrategy(t, dir, nil)
	c1 := capture(0)
	c1.status = http.StatusServiceUnavailable
	first.server = c1
	enqueueNamed(first, "m", 0, 3)
	_ = first.doSync(context.Background())

	second := newDurableStrategy(t, dir, nil)
	c2 := capture(1)
	c2.status = http.StatusAccepted
	second.server = c2
	if err := second.doSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ids[0]) != 3 || !sameStrings(ids[0], ids[1]) {
		t.Fatalf("event ids changed across the restart: %v then %v", ids[0], ids[1])
	}
	for _, id := range ids[0] {
		if id == "" {
			t.Fatal("empty event id")
		}
	}
}

type captureIntake struct {
	status int
	ids    *[]string
}

func (c *captureIntake) Get(string) (*http.Response, error)       { return nil, fmt.Errorf("unused") }
func (c *captureIntake) Post(string, any) (*http.Response, error) { return nil, fmt.Errorf("unused") }
func (c *captureIntake) PostStream(_ string, body string) (*http.Response, error) {
	var events []map[string]any
	_ = json.Unmarshal([]byte(body), &events)
	*c.ids = nil
	for _, e := range events {
		*c.ids = append(*c.ids, e[eventIDField].(string))
	}
	return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func batchOf(prefix string, from, n int) []eventtypes.EventDataPoint {
	out := make([]eventtypes.EventDataPoint, 0, n)
	for i := from; i < from+n; i++ {
		out = append(out, eventtypes.EventDataPoint{
			"timestamp": time.Unix(1_700_000_000+int64(i), 0),
			"host":      "backlog-test-host",
			"severity":  "info",
			"message":   fmt.Sprintf("%s%03d", prefix, i),
		})
	}
	return out
}

func TestBacklogEvictsOldestPastSizeCap(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	dir := t.TempDir()

	probe := newEventBacklog(t.TempDir(), 0, 0, newDurableStrategy(t, t.TempDir(), nil).logger)
	if err := probe.append(batchOf("x", 0, 10), true); err != nil {
		t.Fatal(err)
	}
	_, one := probe.pending()

	b := newEventBacklog(dir, one*5/2, time.Hour, probe.logger)
	for i := 0; i < 5; i++ {
		if err := b.append(batchOf("b", i*10, 10), true); err != nil {
			t.Fatal(err)
		}
	}
	events, size := b.pending()
	if size > b.maxBytes {
		t.Fatalf("disk use %d exceeds the cap %d", size, b.maxBytes)
	}
	if events != 20 || b.files() != 2 {
		t.Fatalf("kept %d events in %d files, want the 2 newest batches (20 events)", events, b.files())
	}
	if d := agentstate.GetPushBufferDropped()["event"]; d != 30 {
		t.Fatalf("dropped counter = %d, want 30", d)
	}

	var got []string
	_, err := b.drain(context.Background(), 10, func(_ context.Context, ev []eventtypes.EventDataPoint) error {
		for _, e := range ev {
			got = append(got, e["message"].(string))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got, names("b", 30, 20)) {
		t.Fatalf("survivors = %v, want b030..b049", got)
	}
}

func TestBacklogEvictsPastMaxAge(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	dir := t.TempDir()
	log := newDurableStrategy(t, t.TempDir(), nil).logger

	clock := time.Unix(1_700_000_000, 0)
	b := newEventBacklog(dir, 0, time.Hour, log)
	b.now = func() time.Time { return clock }

	_ = b.append(batchOf("old", 0, 4), true)
	clock = clock.Add(40 * time.Minute)
	_ = b.append(batchOf("mid", 0, 3), true)
	clock = clock.Add(40 * time.Minute) // old is 80 min, mid 40 min
	b.sweep()

	if n, _ := b.pending(); n != 3 || b.files() != 1 {
		t.Fatalf("after the age sweep: %d events in %d files, want 3 in 1", n, b.files())
	}
	if d := agentstate.GetPushBufferDropped()["event"]; d != 4 {
		t.Fatalf("dropped counter = %d, want 4", d)
	}

	// A restart applies the limit to what an earlier run left.
	clock = clock.Add(2 * time.Hour)
	again := newEventBacklog(dir, 0, time.Hour, log)
	again.now = func() time.Time { return clock }
	again.sweep()
	if n, _ := again.pending(); n != 0 || countBacklogFiles(dir) != 0 {
		t.Fatalf("restart kept %d events, %d files past the age limit", n, countBacklogFiles(dir))
	}
	if d := agentstate.GetPushBufferDropped()["event"]; d != 7 {
		t.Fatalf("dropped counter = %d, want 7", d)
	}
}

func TestBacklogSkipsCorruptAndUnknownVersionFiles(t *testing.T) {
	dir := t.TempDir()
	log := newDurableStrategy(t, t.TempDir(), nil).logger

	good := &backlogBatch{Version: backlogFileVersion, SavedAt: time.Now(), Events: batchOf("g", 0, 2)}
	future := &backlogBatch{Version: backlogFileVersion + 1, SavedAt: time.Now(), Events: batchOf("f", 0, 2)}
	write := func(name string, v any) {
		data, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write(backlogFileName(1), future)
	if err := os.WriteFile(filepath.Join(dir, backlogFileName(2)), []byte("{not json"), 0o640); err != nil {
		t.Fatal(err)
	}
	write(backlogFileName(3), good)

	b := newEventBacklog(dir, 0, 0, log)
	if n, _ := b.pending(); n != 2 || b.files() != 1 {
		t.Fatalf("indexed %d events in %d files, want only the valid one (2 in 1)", n, b.files())
	}
	var got []string
	if _, err := b.drain(context.Background(), 10, func(_ context.Context, ev []eventtypes.EventDataPoint) error {
		for _, e := range ev {
			got = append(got, e["message"].(string))
		}
		return nil
	}); err != nil {
		t.Fatalf("drain must not fail on skipped files: %v", err)
	}
	if !sameStrings(got, names("g", 0, 2)) {
		t.Fatalf("replayed %v, want g000 g001", got)
	}
	if _, err := os.Stat(filepath.Join(dir, backlogFileName(1))); err != nil {
		t.Fatalf("the file of a newer version must stay on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, backlogFileName(2))); err == nil {
		t.Fatal("the corrupt file must be removed")
	}
	if b.seq != 3 {
		t.Fatalf("sequence = %d, want 3 so new files sort after the survivors", b.seq)
	}
}

func TestBacklogUnwritableDirectoryRunsInMemory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newDurableStrategy(t, filepath.Join(blocker, "event-queue"), nil)
	if s.backlog != nil {
		t.Fatal("backlog must be disabled when the directory cannot be written")
	}
	s.openBacklog() // idempotent: no second attempt, no second warning
	if s.backlog != nil {
		t.Fatal("backlog enabled by a second open")
	}

	intake := &ackIntake{status: http.StatusServiceUnavailable}
	s.server = intake
	enqueueNamed(s, "m", 0, 3)
	if err := s.doSync(context.Background()); err == nil {
		t.Fatal("a failed delivery must still be reported")
	}
	if s.failedEvents.Len() != 3 {
		t.Fatalf("in-memory retry holds %d events, want 3", s.failedEvents.Len())
	}
	intake.setStatus(http.StatusAccepted)
	if err := s.doSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !sameStrings(intake.received, names("m", 0, 3)) {
		t.Fatalf("delivered %v", intake.received)
	}
}

func TestBacklogPermanentRefusalIsNotKept(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	dir := t.TempDir()
	intake := &ackIntake{status: http.StatusUnprocessableEntity, dir: dir}
	s := newDurableStrategy(t, dir, nil)
	s.server = intake
	enqueueNamed(s, "m", 0, 4)
	if err := s.doSync(context.Background()); err != nil {
		t.Fatalf("a permanent refusal is not a sync error: %v", err)
	}
	if countBacklogFiles(dir) != 0 {
		t.Fatal("a refused batch must not be queued")
	}
	if d := agentstate.GetPushBufferDropped()["event"]; d != 4 {
		t.Fatalf("dropped = %d, want 4", d)
	}
}

// TestShutdownHonoursContextAgainstHangingIntake is #959: the final flush
// used to ignore the Shutdown context and could run for minutes.
func TestShutdownHonoursContextAgainstHangingIntake(t *testing.T) {
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); hang.Close() })

	dir := t.TempDir()
	s := newDurableStrategy(t, dir, nil)
	s.server = server.NewServer("test-key", hang.URL, testBaseLogger())
	s.retryAttempts = DefaultRetryAttempts
	s.retryDelay = time.Second

	enqueueNamed(s, "m", 0, 6)

	const budget = 400 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	start := time.Now()
	err := s.Shutdown(ctx)
	elapsed := time.Since(start)

	if o := overrun(elapsed, budget, s.backlog); o > shutdownMargin {
		t.Fatalf("Shutdown took %s, %s past its %s deadline (disk writes excluded)", elapsed, o, budget)
	}
	if err == nil {
		t.Fatal("Shutdown against a hanging intake must report that it did not deliver")
	}
	if n, _ := s.backlog.pending(); n != 6 {
		t.Fatalf("%d events on the backlog after Shutdown, want 6", n)
	}
	if countBacklogFiles(dir) == 0 {
		t.Fatal("nothing on disk after Shutdown")
	}
}

// TestShutdownInterruptsRunningSync covers a sync already stuck on the
// intake when Shutdown starts: its batch must end up on disk too.
func TestShutdownInterruptsRunningSync(t *testing.T) {
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); hang.Close() })

	dir := t.TempDir()
	s := newDurableStrategy(t, dir, nil)
	s.server = server.NewServer("test-key", hang.URL, testBaseLogger())

	enqueueNamed(s, "m", 0, 5)
	s.triggerSync()
	time.Sleep(100 * time.Millisecond)

	const budget = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	start := time.Now()
	_ = s.Shutdown(ctx)
	if elapsed := time.Since(start); overrun(elapsed, budget, s.backlog) > shutdownMargin {
		t.Fatalf("Shutdown took %s, past its %s deadline (disk writes excluded)", elapsed, budget)
	}
	if n, _ := s.backlog.pending(); n != 5 {
		t.Fatalf("%d events on disk, want the 5 of the interrupted sync", n)
	}
}

func TestParseBacklogParams(t *testing.T) {
	tests := []struct {
		name    string
		params  configuration.StorageConfigParams
		want    EventSyncStrategyParams
		wantErr bool
	}{
		{"defaults", configuration.StorageConfigParams{}, EventSyncStrategyParams{}, false},
		{"all set", configuration.StorageConfigParams{
			"backlog_path": "/x", "backlog_max_bytes": 1024, "backlog_max_age": "6h",
		}, EventSyncStrategyParams{BacklogPath: "/x", BacklogMaxBytes: 1024, BacklogMaxAge: 6 * time.Hour}, false},
		{"bad size type", configuration.StorageConfigParams{"backlog_max_bytes": "big"}, EventSyncStrategyParams{}, true},
		{"negative size", configuration.StorageConfigParams{"backlog_max_bytes": -1}, EventSyncStrategyParams{}, true},
		{"bad age", configuration.StorageConfigParams{"backlog_max_age": "soon"}, EventSyncStrategyParams{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got EventSyncStrategyParams
			err := parseBacklogParams(tc.params, &got)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBacklogKeepsFileOnTransientReadError(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	dir := t.TempDir()
	log := newDurableStrategy(t, t.TempDir(), nil).logger
	b := newEventBacklog(dir, 0, 0, log)
	if err := b.append(batchOf("m", 0, 3), true); err != nil {
		t.Fatal(err)
	}
	ioErr := fmt.Errorf("too many open files")
	failing := true
	b.readFn = func(path string) ([]byte, error) {
		if failing {
			return nil, ioErr
		}
		return os.ReadFile(path)
	}
	var got []string
	send := func(_ context.Context, ev []eventtypes.EventDataPoint) error {
		for _, e := range ev {
			got = append(got, e["message"].(string))
		}
		return nil
	}

	for round := 0; round < maxReadFailures-1; round++ {
		if _, err := b.drain(context.Background(), 10, send); err != nil {
			t.Fatalf("round %d: a read error must skip the file, not fail the drain: %v", round, err)
		}
		if countBacklogFiles(dir) != 1 {
			t.Fatalf("round %d: a transient read error destroyed a deliverable batch", round)
		}
	}
	failing = false
	if _, err := b.drain(context.Background(), 10, send); err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got, names("m", 0, 3)) || countBacklogFiles(dir) != 0 {
		t.Fatalf("batch not delivered after the I/O error cleared: %v", got)
	}
	if d := agentstate.GetPushBufferDropped()["event"]; d != 0 {
		t.Fatalf("dropped = %d, want 0", d)
	}
}

func TestBacklogGivesUpOnPersistentReadError(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	dir := t.TempDir()
	log := newDurableStrategy(t, t.TempDir(), nil).logger
	b := newEventBacklog(dir, 0, 0, log)
	_ = b.append(batchOf("m", 0, 3), true)
	b.readFn = func(string) ([]byte, error) { return nil, fmt.Errorf("permission denied") }
	send := func(context.Context, []eventtypes.EventDataPoint) error { return nil }
	for round := 0; round < maxReadFailures; round++ {
		_, _ = b.drain(context.Background(), 10, send)
	}
	if countBacklogFiles(dir) != 0 {
		t.Fatal("a file unreadable for good must be given up after the bounded rounds")
	}
	if d := agentstate.GetPushBufferDropped()["event"]; d != 3 {
		t.Fatalf("dropped = %d, want 3 counted", d)
	}
}

func TestShutdownWithBacklogDisabledDoesNotPanic(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newDurableStrategy(t, filepath.Join(blocker, "q"), nil)
	if s.backlog != nil {
		t.Fatal("backlog should be disabled")
	}
	enqueueNamed(s, "m", 0, 3)
	s.syncInProgress.Store(true) // a sync is "running": Shutdown takes the deadline branch
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Shutdown(ctx); err == nil {
		t.Fatal("expected the context error")
	}
}

// blockingIntake does not implement ContextStreamPoster and ignores
// cancellation: it answers only when released.
type blockingIntake struct {
	release chan struct{}
}

func (b *blockingIntake) Get(string) (*http.Response, error)       { return nil, fmt.Errorf("unused") }
func (b *blockingIntake) Post(string, any) (*http.Response, error) { return nil, fmt.Errorf("unused") }
func (b *blockingIntake) PostStream(string, string) (*http.Response, error) {
	<-b.release
	return nil, fmt.Errorf("connection reset")
}

// TestShutdownOwnsParkingOrder: the intake ignores the context, so the
// sync goroutine stays stuck past Shutdown. The in-flight batch must still
// land on disk first, then the buffer, and nothing may be written once
// Shutdown has returned.
func TestShutdownOwnsParkingOrder(t *testing.T) {
	dir := t.TempDir()
	s := newDurableStrategy(t, dir, nil)
	intake := &blockingIntake{release: make(chan struct{})}
	s.server = intake

	enqueueNamed(s, "m", 0, 5)
	s.triggerSync()
	time.Sleep(100 * time.Millisecond) // the goroutine holds the 5 events, stuck in the send
	enqueueNamed(s, "m", 5, 3)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = s.Shutdown(ctx)
	if elapsed := time.Since(start); overrun(elapsed, 200*time.Millisecond, s.backlog) > shutdownMargin {
		t.Fatalf("Shutdown took %s against an intake that ignores the context (disk writes excluded)", elapsed)
	}
	filesAtReturn := countBacklogFiles(dir)

	close(intake.release) // the stuck goroutine now wakes up
	time.Sleep(300 * time.Millisecond)
	if got := countBacklogFiles(dir); got != filesAtReturn {
		t.Fatalf("files on disk went from %d to %d after Shutdown returned", filesAtReturn, got)
	}

	var got []string
	b := newEventBacklog(dir, 0, 0, s.logger)
	if _, err := b.drain(context.Background(), 100, func(_ context.Context, ev []eventtypes.EventDataPoint) error {
		for _, e := range ev {
			got = append(got, e["message"].(string))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got, names("m", 0, 8)) {
		t.Fatalf("on-disk order = %v, want m000..m007 (in-flight batch first, then the buffer)", got)
	}
}

// captureLogger returns a module logger whose lines land in the returned
// buffer, and a function counting those that contain a text.
func captureLogger() (*logger.ModuleLogger, func(string) int) {
	var buf lockedBuffer
	base := zerolog.New(&buf)
	return logger.NewModuleLogger(&base, "strategy.event"), func(text string) int {
		return strings.Count(buf.String(), text)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestBacklogEvictionWarnsOncePerMinute: a cap hit on every append used to
// log one line per eviction, 66 in ten minutes on the container bench.
func TestBacklogEvictionWarnsOncePerMinute(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	log, lines := captureLogger()
	clock := time.Unix(1_700_000_000, 0)

	probe := newEventBacklog(t.TempDir(), 0, 0, log)
	_ = probe.append(batchOf("x", 0, 10), true)
	_, one := probe.pending()

	b := newEventBacklog(t.TempDir(), one*5/2, time.Hour, log)
	b.now = func() time.Time { return clock }
	const batches = 60
	for i := 0; i < batches; i++ {
		clock = clock.Add(time.Second)
		if err := b.append(batchOf("b", i*10, 10), true); err != nil {
			t.Fatal(err)
		}
	}
	// 58 evictions in 60 s: the first one is reported, the rest wait.
	if got := lines("over its limits"); got != 1 {
		t.Fatalf("%d warnings for evictions inside one minute, want 1", got)
	}

	clock = clock.Add(2 * time.Minute)
	b.sweep()
	if got := lines("over its limits"); got != 2 {
		t.Fatalf("%d warnings after the minute, want 2 (the summary of the rest)", got)
	}
	clock = clock.Add(2 * time.Minute)
	b.sweep()
	if got := lines("over its limits"); got != 2 {
		t.Fatalf("a sweep with nothing new warned again: %d lines", got)
	}
	// The counter stays exact whatever the log says.
	if d := agentstate.GetPushBufferDropped()["event"]; d != uint64(batches-2)*10 {
		t.Fatalf("dropped counter = %d, want %d", d, (batches-2)*10)
	}
}

func TestBacklogWriteFailureWarnsOncePerMinute(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	dir := t.TempDir()
	s := newDurableStrategy(t, dir, nil)
	log, lines := captureLogger()
	s.logger = log
	s.backlog.logger = log
	// Replace the directory by a file: every append fails.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		s.persist(batchOf("m", i*3, 3), true)
	}
	if got := lines("could not be written"); got != 1 {
		t.Fatalf("%d write-failure warnings in a burst, want 1", got)
	}
	if d := agentstate.GetPushBufferDropped()["event"]; d != 60 {
		t.Fatalf("dropped counter = %d, want 60", d)
	}
	if s.unwarned.Load() != 57 {
		t.Fatalf("unreported events = %d, want the 57 behind the first warning", s.unwarned.Load())
	}
}
