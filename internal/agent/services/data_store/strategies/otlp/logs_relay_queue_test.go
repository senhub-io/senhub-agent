package otlp

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

type logsRelayRig struct {
	t     *testing.T
	col   *fakeCollector
	relay *logsRelay
	q     *wireQueue
	ch    *wireChannel
	rp    *wireReplayer
}

// newLogsRelayRig wires a logs relay to its own queue the way the strategy
// does, with the replay driven by the test.
func newLogsRelayRig(t *testing.T, col *fakeCollector, dir string) *logsRelayRig {
	t.Helper()
	cfg := relayTestConfig(col.endpoint, col.protocol)
	cfg.Logs.Enabled = true
	cfg.Compression = "none"
	log := testModuleLogger(t)
	relay, err := newLogsRelay(cfg, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.forwarder.close() })
	q := newWireQueue(logsRelayQueueKind, dir, 0, log)
	ch := newWireChannel("logs_relay", q, false, false, log)
	ch.successKey = "otlp/logs"
	ch.replaySend = relay.replay
	relay.queue = ch
	return &logsRelayRig{t: t, col: col, relay: relay, q: q, ch: ch, rp: newWireReplayer(ch, log)}
}

func (r *logsRelayRig) files() int { return len(r.q.files()) }

// bodies is the body of the first record of every request acknowledged.
func (c *fakeCollector) logBodies() []string {
	var out []string
	for _, req := range c.receivedLogs() {
		for _, rl := range req.GetResourceLogs() {
			for _, sl := range rl.GetScopeLogs() {
				for _, rec := range sl.GetLogRecords() {
					out = append(out, rec.GetBody().GetStringValue())
				}
			}
		}
	}
	return out
}

// ent#973: a relayed log batch the collector did not take used to be
// dropped. It is kept on disk with the application's own Resource and
// replayed in order once the collector answers.
func TestLogsRelay_FailedBatchKeptAndReplayedInOrder(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newLogsRelayRig(t, col, t.TempDir())
			errorsBefore := exportErrorCount("logs")

			col.setDown(true)
			rig.relay.export(relayLogBatch("checkout-api", "first"), 1)
			rig.relay.export(relayLogBatch("checkout-api", "second"), 1)
			rig.relay.export(relayLogBatch("checkout-api", "third"), 1)
			if rig.files() != 3 {
				t.Fatalf("kept %d relayed batches, want 3", rig.files())
			}
			if n := col.attemptCount(); n != 1 {
				t.Errorf("backend attempted %d times, want 1 (the others skip a known-down backend)", n)
			}
			if got := exportErrorCount("logs") - errorsBefore; got != 1 {
				t.Errorf("export errors moved by %d, want 1 (the one live attempt)", got)
			}
			if got := agentstate.GetOTLPQueueStats("logs_relay").Records; got != 3 {
				t.Errorf("queue gauge = %d records, want 3", got)
			}

			col.setDown(false)
			if n, err := rig.rp.replay(); err != nil || n != 3 {
				t.Fatalf("replay = %d, %v", n, err)
			}
			got := col.logBodies()
			if len(got) != 3 || got[0] != "first" || got[1] != "second" || got[2] != "third" {
				t.Errorf("delivered %v, want first, second, third in order", got)
			}
			for _, req := range col.receivedLogs() {
				name := ""
				for _, kv := range req.GetResourceLogs()[0].GetResource().GetAttributes() {
					if kv.GetKey() == "service.name" {
						name = kv.GetValue().GetStringValue()
					}
				}
				if name != "checkout-api" {
					t.Errorf("replayed request carries service.name %q, want the application's own", name)
				}
			}
			if rig.files() != 0 {
				t.Errorf("%d files left after an acknowledged replay", rig.files())
			}
		})
	}
}

// Without a queue the relay still drops a failed batch, as before.
func TestLogsRelay_NoQueueStillDrops(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newLogsRelayRig(t, col, t.TempDir())
	rig.relay.queue = nil
	col.setDown(true)
	before := exportErrorCount("logs")
	rig.relay.export(relayLogBatch("app", "lost"), 1)
	if got := exportErrorCount("logs") - before; got != 1 {
		t.Errorf("export errors moved by %d, want 1", got)
	}
	if rig.files() != 0 {
		t.Error("a batch was queued with no queue")
	}
}

// A relayed batch the collector throttles is kept, waits for the hint, and
// is neither an export error nor one warning per batch.
func TestLogsRelay_ThrottleKeepsBatchAndHonoursHint(t *testing.T) {
	cases := []struct {
		protocol, retryAfter string
		retryDelay           time.Duration
	}{
		{"http", "30", 0},
		{"grpc", "", 30 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.protocol, func(t *testing.T) {
			col := newFakeCollector(t, c.protocol)
			rig := newLogsRelayRig(t, col, t.TempDir())
			col.throttleFor(time.Now().Add(time.Minute), c.retryAfter, c.retryDelay)
			throttledBefore, errorsBefore := throttledCount("logs"), exportErrorCount("logs")

			rig.relay.export(relayLogBatch("app", "one"), 1)
			rig.relay.export(relayLogBatch("app", "two"), 1)
			if rig.files() != 2 {
				t.Fatalf("kept %d batches, want 2", rig.files())
			}
			if n := col.attemptCount(); n != 1 {
				t.Errorf("collector asked %d times, want 1", n)
			}
			if rem := rig.ch.throttleRemaining(); rem <= 25*time.Second || rem > 30*time.Second {
				t.Errorf("remaining wait = %v, want about 30s", rem)
			}
			if got := throttledCount("logs") - throttledBefore; got != 1 {
				t.Errorf("throttled counter moved by %d, want 1", got)
			}
			if got := exportErrorCount("logs") - errorsBefore; got != 0 {
				t.Errorf("export errors moved by %d, want 0", got)
			}
		})
	}
}

// A stored request that cannot be read is discarded, not retried forever at
// the head of the queue.
func TestLogsRelay_UnreadableStoredRequestIsDiscarded(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newLogsRelayRig(t, col, t.TempDir())
	if err := rig.q.enqueue([]byte{0xff, 0xff, 0xff}, 1); err != nil {
		t.Fatal(err)
	}
	rig.relay.export(relayLogBatch("app", "behind"), 1)
	rig.ch.healthy.Store(true)
	if n, err := rig.rp.replay(); err != nil {
		t.Fatalf("replay = %d, %v", n, err)
	}
	if rig.files() != 0 {
		t.Errorf("%d files left, want the unreadable one discarded and the next delivered", rig.files())
	}
}

// The strategy wires the relay queue when the logs queue is on.
func TestStrategy_LogsRelayQueueIsWired(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	dir := t.TempDir()
	params := queueStrategyParams(col, dir)
	params["signals"].(map[string]interface{})["logs"] = map[string]interface{}{"enabled": true}
	s := newTestStrategy(t, params)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()
	if s.logsRelay == nil || s.logsRelay.queue == nil || s.logsRelayChan == nil {
		t.Fatal("the logs relay has no queue although the logs queue is on")
	}
}
