package hostpoll

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

type fakeCollector struct {
	points     []datapoint.DataPoint
	collectErr error
	closeErr   error
}

func (f *fakeCollector) Collect(time.Time) ([]datapoint.DataPoint, error) {
	if f.collectErr != nil {
		return nil, f.collectErr
	}
	return f.points, nil
}

func (f *fakeCollector) Close() error { return f.closeErr }

func testSpec(c Collector, err error) Spec {
	return Spec{
		Module:   "probe.test",
		Subject:  "Test",
		TypeName: "TestProbe",
		NewCollector: func(map[string]interface{}, *logger.Logger, *logger.ModuleLogger) (Collector, error) {
			return c, err
		},
	}
}

func newTestProbe(t *testing.T, config map[string]interface{}, c Collector) *Probe {
	t.Helper()
	p, err := New(config, logger.NewLogger(&cliArgs.ParsedArgs{}), testSpec(c, nil))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return p
}

// The interval is read through types.IntParam, so every numeric encoding a
// YAML or JSON decode can hand over is honoured — the `config["interval"].(int)`
// assertion this base replaced silently fell back to the default for all but
// the plain int (#136).
func TestNewReadsIntervalInEveryNumericEncoding(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   time.Duration
	}{
		{"absent falls back to the default", map[string]interface{}{}, 30 * time.Second},
		{"yaml int literal", map[string]interface{}{"interval": 60}, 60 * time.Second},
		{"json float64", map[string]interface{}{"interval": float64(45)}, 45 * time.Second},
		{"int64", map[string]interface{}{"interval": int64(15)}, 15 * time.Second},
		{"numeric string", map[string]interface{}{"interval": "90"}, 90 * time.Second},
		{"unusable value falls back to the default", map[string]interface{}{"interval": "often"}, 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProbe(t, tc.config, &fakeCollector{})
			if got := p.GetInterval(); got != tc.want {
				t.Errorf("GetInterval() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewWrapsCollectorConstructionError(t *testing.T) {
	_, err := New(nil, logger.NewLogger(&cliArgs.ParsedArgs{}), testSpec(nil, errors.New("no counters")))
	if err == nil {
		t.Fatal("New() error = nil, want the collector construction failure")
	}
	if err.Error() != "failed to create Test collector: no counters" {
		t.Errorf("New() error = %q, want the Subject-qualified message", err.Error())
	}
}

func TestCollectEnrichesWithProbeIdentity(t *testing.T) {
	p := newTestProbe(t, nil, &fakeCollector{
		points: []datapoint.DataPoint{{Name: "system.cpu.utilization", Value: 12.5}},
	})
	p.SetName("cpu2")
	p.SetProbeType("cpu")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("Collect() returned %d points, want 1", len(points))
	}

	got := map[string]string{}
	for _, tag := range points[0].Tags {
		got[tag.Key] = tag.Value
	}
	if got["probe_name"] != "cpu2" || got["probe_type"] != "cpu" {
		t.Errorf("Collect() tags = %v, want probe_name=cpu2 and probe_type=cpu", got)
	}
}

func TestCollectQualifiesTheCollectorFailure(t *testing.T) {
	p := newTestProbe(t, nil, &fakeCollector{collectErr: errors.New("boom")})
	if _, err := p.Collect(); err == nil || err.Error() != "failed to collect Test metrics: boom" {
		t.Errorf("Collect() error = %v, want the Subject-qualified message", err)
	}
}

func TestOnShutdownClosesTheCollector(t *testing.T) {
	closeErr := errors.New("close failed")
	p := newTestProbe(t, nil, &fakeCollector{closeErr: closeErr})
	if err := p.OnShutdown(context.Background()); !errors.Is(err, closeErr) {
		t.Errorf("OnShutdown() error = %v, want %v", err, closeErr)
	}
}

func TestStringCarriesTheProbeTypeName(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"interval": 10}, &fakeCollector{})
	p.SetName("test1")
	if got, want := p.String(), "TestProbe{name=test1, interval=10s}"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
