package probes

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/types/datapoint"
)

type deadlineProbe struct {
	*erroringProbe
	interval time.Duration
	timeout  time.Duration
	seen     chan time.Time
}

func (d *deadlineProbe) GetInterval() time.Duration { return d.interval }
func (d *deadlineProbe) CollectTimeout() time.Duration {
	return d.timeout
}
func (d *deadlineProbe) CollectContext(ctx context.Context) ([]datapoint.DataPoint, error) {
	at, _ := ctx.Deadline()
	d.seen <- at
	return nil, nil
}

func newDeadlineProbe(interval, timeout time.Duration) *deadlineProbe {
	base := &types.BaseProbe{}
	base.SetName("deadline")
	base.SetProbeType("deadline_stub")
	return &deadlineProbe{
		erroringProbe: &erroringProbe{BaseProbe: base},
		interval:      interval,
		timeout:       timeout,
		seen:          make(chan time.Time, 1),
	}
}

func TestCollectDeadlineIsTheShorterOfIntervalAndOwnTimeout(t *testing.T) {
	cases := []struct {
		name              string
		interval, timeout time.Duration
		want              time.Duration
	}{
		{"interval only", 60 * time.Second, 0, 60 * time.Second},
		{"timeout shorter", 60 * time.Second, 10 * time.Second, 10 * time.Second},
		{"timeout longer", 30 * time.Second, 90 * time.Second, 30 * time.Second},
		{"no interval, own timeout", 0, 5 * time.Second, 5 * time.Second},
		{"neither", 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := collectDeadline(newDeadlineProbe(c.interval, c.timeout)); got != c.want {
				t.Errorf("collectDeadline = %s, want %s", got, c.want)
			}
		})
	}
}

func TestCollectHandsAContextCollectorItsDeadline(t *testing.T) {
	probe := newDeadlineProbe(time.Minute, 0)
	poller := &ProbePoller{
		ProbeId:      "deadline-test",
		Probe:        probe,
		moduleLogger: testModuleLogger(t),
		addDataPoint: func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), collectDeadline(probe))
	defer cancel()
	want, _ := ctx.Deadline()
	if err := poller.collect(ctx); err != nil {
		t.Fatal(err)
	}
	if got := <-probe.seen; got.IsZero() || got.Sub(want) > time.Second {
		t.Errorf("CollectContext saw deadline %v, want about %v", got, want)
	}
}
