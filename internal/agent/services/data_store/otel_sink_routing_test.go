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
