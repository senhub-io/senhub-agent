package sensor

import (
	"context"
	"net"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// A listener whose configuration changes must still be listening after
// the reload. The replacement used to start beside its predecessor, find
// the port taken, fail, and the predecessor was then stopped: nothing
// listened until the retry two minutes later (otlp_receiver on the beta 2
// retest, after adding `signals`).
func TestAReconfiguredListenerKeepsItsPort(t *testing.T) {
	probe, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	_ = probe.Close()

	provider := &MockConfigProvider{}
	syslogWith := func(interval int) []configuration.ProbeConfig {
		return []configuration.ProbeConfig{{Name: "syslog", Type: "syslog", Params: map[string]interface{}{
			"port": port, "protocol": "udp", "bind_address": "127.0.0.1", "interval": interval,
		}}}
	}
	provider.config.Probes = syslogWith(60)
	add := func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil }
	s := NewSensor(add, provider, logger.NewLogger(&cliArgs.ParsedArgs{})).(*sensor)
	t.Cleanup(func() {
		for _, p := range s.startedProbes {
			_ = p.Shutdown(context.Background())
		}
	})

	if err := s.SyncConfiguration(); err != nil || len(s.failedProbes) != 0 {
		t.Fatalf("first start: err=%v failed=%v", err, s.failedProbes)
	}

	provider.config.Probes = syslogWith(90)
	if err := s.SyncConfiguration(); err != nil {
		t.Fatal(err)
	}
	if len(s.failedProbes) != 0 || len(s.startedProbes) != 1 {
		t.Fatalf("after the change: %d running, failed=%v", len(s.startedProbes), s.failedProbes)
	}
	if c, err := net.ListenPacket("udp", probe.LocalAddr().String()); err == nil {
		_ = c.Close()
		t.Fatal("nothing listens on the port after the reconfiguration")
	}
}
