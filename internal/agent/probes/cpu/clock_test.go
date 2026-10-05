package cpu

import (
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
)

type stubCollector struct {
	points []datapoint.DataPoint
	err    error
}

func (s stubCollector) Collect(time.Time) ([]datapoint.DataPoint, error) { return s.points, s.err }
func (s stubCollector) Close() error                                     { return nil }

func TestWithClockAddsTheHostClock(t *testing.T) {
	sampled := time.Unix(1_800_000_000, 250_000_000)
	c := withClock{Collector: stubCollector{}, now: func() time.Time { return sampled }}

	points, err := c.Collect(time.Unix(1_799_999_999, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Name != "system_time" {
		t.Fatalf("points = %+v", points)
	}
	if got := points[0].Value; got != 1_800_000_000.25 {
		t.Errorf("value = %v, want the sampled time as fractional epoch seconds", got)
	}
}

func TestWithClockDoesNotMaskACollectorFailure(t *testing.T) {
	boom := errors.New("boom")
	c := withClock{Collector: stubCollector{err: boom}, now: time.Now}
	points, err := c.Collect(time.Now())
	if !errors.Is(err, boom) || len(points) != 0 {
		t.Fatalf("got %v, %v", points, err)
	}
}
