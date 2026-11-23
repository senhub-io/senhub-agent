package otlp

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// Traces fail over like metrics and logs: with the primary down and the
// standby up, live spans and the backlog both reach the standby.
func TestTracesFailover_SpansAndBacklogReachTheStandby(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			primary := newFakeCollector(t, proto)
			standby := newFakeCollector(t, proto)
			dir := t.TempDir()
			cfg := wireTestConfig(primary, dir)
			cfg.FallbackEndpoints = []string{standby.endpoint}
			cfg.FailoverCooldown = time.Hour
			exp, err := buildFailoverTraceExporter(context.Background(), cfg, testModuleLogger(t))
			if err != nil {
				t.Fatal(err)
			}
			q := newWireQueue(tracesQueueKind, dir, 0, testModuleLogger(t))
			ch := newWireChannel("traces", q, false, true, testModuleLogger(t))
			rig := &wireRig{t: t, col: primary, signal: "traces", q: q, ch: ch, sexp: newPersistentSpanExporter(exp, ch)}
			rig.rp = newWireReplayer(ch, testModuleLogger(t))
			t.Cleanup(func() { _ = rig.sexp.Shutdown(context.Background()) })

			// The primary is down; the standby answers the live span.
			primary.setDown(true)
			if err := rig.pushSpan("live-on-standby"); err != nil {
				t.Fatalf("push: %v", err)
			}
			if got := standby.spanNames(); len(got) != 1 || got[0] != "live-on-standby" {
				t.Fatalf("standby received %v", got)
			}

			// Both down: the span is kept; the standby back, the replay
			// delivers it there.
			standby.setDown(true)
			_ = rig.pushSpan("kept")
			if rig.files() != 1 {
				t.Fatalf("files = %d, want 1", rig.files())
			}
			standby.setDown(false)
			ch.healthy.Store(true)
			if n, err := rig.rp.replay(); err != nil || n != 1 {
				t.Fatalf("replay = %d, %v", n, err)
			}
			if got := standby.spanNames(); len(got) != 2 || got[1] != "kept" {
				t.Errorf("standby received %v, want the backlog too", got)
			}
			if got := len(primary.receivedTraces()); got != 0 {
				t.Errorf("primary received %d while down", got)
			}
			_ = agentstate.GetOTLPQueueStats("traces")
		})
	}
}

// The relays follow the failover too: a relayed log batch goes to the
// standby when the primary is down.
func TestLogsRelay_FailoverReachesTheStandby(t *testing.T) {
	primary := newFakeCollector(t, "grpc")
	standby := newFakeCollector(t, "grpc")
	primary.setDown(true)
	cfg := relayTestConfig(primary.endpoint, "grpc")
	cfg.Logs.Enabled = true
	cfg.Compression = "none"
	cfg.FallbackEndpoints = []string{standby.endpoint}
	relay, err := newLogsRelay(cfg, nil, testModuleLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.forwarder.close() }()
	relay.export(relayLogBatch("app", "to-standby"), 1)
	if got := standby.logBodies(); len(got) != 1 || got[0] != "to-standby" {
		t.Errorf("standby received %v", got)
	}
}
