package filetail

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestFileTailProbe(t *testing.T) *FileTailProbe {
	t.Helper()
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	p := &FileTailProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.filetail"),
	}
	p.SetProbeType(ProbeType)
	return p
}

func recordsEmitted(t *testing.T, p *FileTailProbe) float64 {
	t.Helper()
	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range pts {
		if dp.Name == "senhub.filetail.records_emitted" {
			return dp.Value
		}
	}
	t.Fatalf("Collect did not emit senhub.filetail.records_emitted; got %+v", pts)
	return 0
}

func TestFileTail_RecordsEmittedSelfMetric(t *testing.T) {
	p := newTestFileTailProbe(t)

	if got := recordsEmitted(t, p); got != 0 {
		t.Errorf("initial records_emitted = %v, want 0", got)
	}

	pc := ParserConfig{Type: ParserRaw}
	for i := 0; i < 3; i++ {
		p.publish(pc, "a log line", time.Now(), p.GetName(), "/var/log/app.log")
	}

	if got := recordsEmitted(t, p); got != 3 {
		t.Errorf("records_emitted after 3 publishes = %v, want 3", got)
	}
}

func TestFileTail_SelfMetricRoutesToOTLP(t *testing.T) {
	// Regression for #701: the conduit's self-metric must route to the
	// metric sinks (it used to return []string{}, dropping any datapoint).
	p := newTestFileTailProbe(t)
	has := false
	for _, s := range p.GetTargetStrategies() {
		if s == "otlp" {
			has = true
		}
	}
	if !has {
		t.Errorf("GetTargetStrategies %v must include otlp", p.GetTargetStrategies())
	}
}
