package filetail

import (
	"path/filepath"
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

func TestFileTail_FileRecordsEmittedIsCountedPerFile(t *testing.T) {
	dir := t.TempDir()
	busy := filepath.Join(dir, "busy.log")
	quiet := filepath.Join(dir, "quiet.log")
	writeFile(t, busy, "x\n")
	writeFile(t, quiet, "x\n")

	p := newTestFileTailProbe(t)
	p.tailing = map[string]*tailState{busy: {}, quiet: {}}

	pc := ParserConfig{Type: ParserRaw}
	for i := 0; i < 4; i++ {
		p.publish(pc, "a log line", time.Now(), p.GetName(), busy)
	}

	pts := collectPoints(t, p)
	if got, ok := positionOf(t, pts, "senhub.filetail.file_records_emitted", busy); !ok || got != 4 {
		t.Errorf("busy file: file_records_emitted = %v (reported %v), want 4", got, ok)
	}
	if got, ok := positionOf(t, pts, "senhub.filetail.file_records_emitted", quiet); !ok || got != 0 {
		t.Errorf("quiet file: file_records_emitted = %v (reported %v), want 0 reported", got, ok)
	}
	if got := recordsEmitted(t, p); got != 4 {
		t.Errorf("probe-wide records_emitted = %v, want 4", got)
	}
}
