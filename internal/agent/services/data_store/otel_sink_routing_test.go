package data_store

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// otelSinkStrategy stands for an output that consumes the mapper-shaped
// metrics, the way the Zabbix output does.
type otelSinkStrategy struct{ MockStrategy }

func (s *otelSinkStrategy) ConsumesOtelMetrics() bool { return true }

// The probes name their targets in a list written before such sinks
// existed; a sink of this kind receives what otlp receives, and nothing
// when the probe does not target otlp.
func TestGetCallback_AnOtelMetricSinkReceivesWhatOTLPReceives(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	ds := NewDataStore(&MockAgentConfig{}, &MockConfigProvider{}, baseLogger).(*dataStore)

	sink := &otelSinkStrategy{MockStrategy{name: "zabbix"}}
	plain := &MockStrategy{name: "event"}
	func() { v := []SyncStrategy{sink, plain}; ds.strategies.Store(&v) }()
	callback := ds.GetCallback()
	data := []datapoint.DataPoint{{Name: "test", Value: 1.0, Timestamp: time.Now()}}

	if err := callback(data, &MockStrategyRouter{targets: []string{"senhub", "prtg", "http", "otlp"}}); err != nil {
		t.Fatal(err)
	}
	if len(sink.dataPoints) != 1 {
		t.Errorf("a probe targeting otlp must reach the sink, got %d batches", len(sink.dataPoints))
	}
	if len(plain.dataPoints) != 0 {
		t.Errorf("an output that is not targeted and not a sink must get nothing, got %d", len(plain.dataPoints))
	}

	if err := callback(data, &MockStrategyRouter{targets: []string{"event"}}); err != nil {
		t.Fatal(err)
	}
	if len(sink.dataPoints) != 1 {
		t.Errorf("a probe that does not target otlp must not reach the sink, got %d batches", len(sink.dataPoints))
	}
}

// cadenceSink records the cadence hints an output receives.
type cadenceSink struct {
	MockStrategy
	cadence map[string]time.Duration
}

func (s *cadenceSink) NoteProbeCadence(name string, interval time.Duration) {
	s.cadence[name] = interval
}

// periodicRouter is a probe that exposes its name and interval, the way
// every scheduler-driven probe does.
type periodicRouter struct{ *MockStrategyRouter }

func (periodicRouter) GetName() string            { return "reboot-check" }
func (periodicRouter) GetInterval() time.Duration { return 30 * time.Minute }

// An output that asks for it learns the cadence of every probe whose
// data it receives; a probe without a rhythm (callback-driven) tells
// it nothing.
func TestGetCallback_TellsACadenceSinkHowOftenTheProbeCollects(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	ds := NewDataStore(&MockAgentConfig{}, &MockConfigProvider{}, baseLogger).(*dataStore)

	sink := &cadenceSink{MockStrategy: MockStrategy{name: "otlp"}, cadence: map[string]time.Duration{}}
	func() { v := []SyncStrategy{sink}; ds.strategies.Store(&v) }()
	callback := ds.GetCallback()
	data := []datapoint.DataPoint{{Name: "test", Value: 1.0, Timestamp: time.Now()}}

	if err := callback(data, periodicRouter{&MockStrategyRouter{targets: []string{"otlp"}}}); err != nil {
		t.Fatal(err)
	}
	if got := sink.cadence["reboot-check"]; got != 30*time.Minute {
		t.Errorf("cadence = %v, want 30m", got)
	}
	if err := callback(data, &MockStrategyRouter{targets: []string{"otlp"}}); err != nil {
		t.Fatal(err)
	}
	if len(sink.cadence) != 1 {
		t.Errorf("a probe without a rhythm must add nothing, got %v", sink.cadence)
	}
}
