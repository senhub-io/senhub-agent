package otlpreceiver

import (
	"reflect"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestCoverageOrigins_FollowTheListener(t *testing.T) {
	cases := []struct {
		name string
		cfg  receiverConfig
		want []string
	}{
		{"unix socket", receiverConfig{Address: "unix:/run/x.sock", UnixPath: "/run/x.sock"}, []string{originUDS}},
		{"loopback", receiverConfig{Address: "127.0.0.1:4317"}, []string{originTCPLoopback}},
		{"wildcard", receiverConfig{Address: "0.0.0.0:4317"}, []string{originTCPLoopback, originRemote}},
	}
	for _, c := range cases {
		p := &OTLPReceiverProbe{config: c.cfg}
		if got := p.coverageOrigins(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: origins %v, want %v", c.name, got, c.want)
		}
	}
}

// A receiver that has taken nothing reads as zero for every enabled signal,
// not as absent (#591).
func TestSeedOTLPReceiverCoverage_ZeroSeriesPerSignal(t *testing.T) {
	agentstate.ResetOTLPReceiverCoverageForTest()
	t.Cleanup(agentstate.ResetOTLPReceiverCoverageForTest)

	agentstate.SeedOTLPReceiverCoverage([]string{signalMetrics, signalLogs}, []string{originUDS})
	got := agentstate.GetOTLPReceiverCoverage()
	for _, sig := range []string{signalMetrics, signalLogs} {
		c, ok := got[agentstate.OTLPReceiverCoverageKey{Signal: sig, Origin: originUDS, Service: "unknown"}]
		if !ok || c.Received != 0 || c.WithoutHostID != 0 {
			t.Errorf("signal %s: want a zero series, got %+v present=%v", sig, c, ok)
		}
	}
	if _, ok := got[agentstate.OTLPReceiverCoverageKey{Signal: signalTraces, Origin: originUDS, Service: "unknown"}]; ok {
		t.Error("a signal that is not enabled must not be seeded")
	}
}

func TestSeedOTLPReceiverCoverage_NeverLowersACount(t *testing.T) {
	agentstate.ResetOTLPReceiverCoverageForTest()
	t.Cleanup(agentstate.ResetOTLPReceiverCoverageForTest)

	agentstate.RecordOTLPReceiverCoverage(signalLogs, originUDS, "", 5, true)
	agentstate.SeedOTLPReceiverCoverage([]string{signalLogs}, []string{originUDS})
	key := agentstate.OTLPReceiverCoverageKey{Signal: signalLogs, Origin: originUDS, Service: "unknown"}
	if c := agentstate.GetOTLPReceiverCoverage()[key]; c.Received != 5 {
		t.Errorf("seeding changed an existing count to %d", c.Received)
	}
}
