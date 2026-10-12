package cpu

import (
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/tags"
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

func TestWithClockAddsTheUptimeWhenASourceIsGiven(t *testing.T) {
	c := withClock{
		Collector: stubCollector{},
		now:       time.Now,
		uptime:    func() (uint64, error) { return 86_400, nil },
	}
	points, err := c.Collect(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var got float64 = -1
	for _, p := range points {
		if p.Name == "system_uptime" {
			got = p.Value
		}
	}
	if got != 86_400 {
		t.Errorf("uptime = %v, want 86400", got)
	}
}

func TestAnUptimeFailureKeepsTheClockAndIsReported(t *testing.T) {
	boom := errors.New("no boot time")
	var reported error
	c := withClock{
		Collector:     stubCollector{},
		now:           time.Now,
		uptime:        func() (uint64, error) { return 0, boom },
		onUptimeError: func(err error) { reported = err },
	}
	points, err := c.Collect(time.Now())
	if err != nil {
		t.Fatalf("an unreadable uptime must not fail the cycle: %v", err)
	}
	if len(points) != 1 || points[0].Name != "system_time" {
		t.Errorf("the clock point must survive: %+v", points)
	}
	if !errors.Is(reported, boom) {
		t.Errorf("the failure was not reported: %v", reported)
	}
}

func pointByName(points []datapoint.DataPoint, name string) (datapoint.DataPoint, bool) {
	for _, p := range points {
		if p.Name == name {
			return p, true
		}
	}
	return datapoint.DataPoint{}, false
}

func hasTag(tagList []tags.Tag, key, value string) bool {
	for _, tag := range tagList {
		if tag.Key == key && tag.Value == value {
			return true
		}
	}
	return false
}

func TestWithClockAddsSyncPointsWhenASourceIsGiven(t *testing.T) {
	c := withClock{
		Collector: stubCollector{},
		now:       time.Now,
		sync: func() (clockSync, error) {
			return clockSync{
				source:   "chrony",
				synced:   true,
				offset:   1500 * time.Microsecond,
				maxError: 500 * time.Microsecond,
			}, nil
		},
	}

	points, err := c.Collect(time.Now())
	if err != nil {
		t.Fatal(err)
	}

	synchronized, ok := pointByName(points, "system_clock_synchronized")
	if !ok {
		t.Fatalf("system_clock_synchronized missing from %+v", points)
	}
	if synchronized.Value != 1 {
		t.Errorf("synchronized = %v, want 1", synchronized.Value)
	}
	if !hasTag(synchronized.Tags, "clock_source", "chrony") {
		t.Errorf("synchronized tags = %+v, want clock_source=chrony", synchronized.Tags)
	}

	offset, ok := pointByName(points, "system_clock_offset")
	if !ok {
		t.Fatalf("system_clock_offset missing from %+v", points)
	}
	if offset.Value != 0.0015 {
		t.Errorf("offset = %v, want 0.0015 s", offset.Value)
	}
	if !hasTag(offset.Tags, "clock_source", "chrony") {
		t.Errorf("offset tags = %+v, want clock_source=chrony", offset.Tags)
	}

	maxError, ok := pointByName(points, "system_clock_max_error")
	if !ok {
		t.Fatalf("system_clock_max_error missing from %+v", points)
	}
	if maxError.Value != 0.0005 {
		t.Errorf("max_error = %v, want 0.0005 s", maxError.Value)
	}
	if !hasTag(maxError.Tags, "clock_source", "chrony") {
		t.Errorf("max_error tags = %+v, want clock_source=chrony", maxError.Tags)
	}
}

func TestWithClockLeavesSyncPointsOutWhenNoSourceIsGiven(t *testing.T) {
	c := withClock{Collector: stubCollector{}, now: time.Now}
	points, err := c.Collect(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"system_clock_synchronized", "system_clock_offset", "system_clock_max_error"} {
		if _, ok := pointByName(points, name); ok {
			t.Fatalf("unexpected metric %s: %+v", name, points)
		}
	}
}

func TestSyncFailureIsReportedOncePerErrorText(t *testing.T) {
	boom := errors.New("adjtimex boom")
	var reported []error
	c := withClock{
		Collector: stubCollector{},
		now:       time.Now,
		sync:      func() (clockSync, error) { return clockSync{}, boom },
		onSyncError: func(err error) {
			reported = append(reported, err)
		},
	}

	for i := 0; i < 2; i++ {
		points, err := c.Collect(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := pointByName(points, "system_time"); !ok {
			t.Fatal("system_time point must survive a sync failure")
		}
		if _, ok := pointByName(points, "system_clock_synchronized"); ok {
			t.Fatal("sync points must be absent when sync fails")
		}
	}

	if len(reported) != 1 {
		t.Fatalf("reported %d errors, want 1", len(reported))
	}
	if !errors.Is(reported[0], boom) {
		t.Errorf("reported error = %v, want boom", reported[0])
	}
}

func TestOffsetIsOmittedWhenClockIsUnsynchronised(t *testing.T) {
	c := withClock{
		Collector: stubCollector{},
		now:       time.Now,
		sync: func() (clockSync, error) {
			return clockSync{
				source:   "ntpd",
				synced:   false,
				offset:   4 * time.Second,
				maxError: 2 * time.Second,
			}, nil
		},
	}

	points, err := c.Collect(time.Now())
	if err != nil {
		t.Fatal(err)
	}

	synchronized, ok := pointByName(points, "system_clock_synchronized")
	if !ok {
		t.Fatalf("system_clock_synchronized missing from %+v", points)
	}
	if synchronized.Value != 0 {
		t.Errorf("synchronized = %v, want 0", synchronized.Value)
	}
	if !hasTag(synchronized.Tags, "clock_source", "ntpd") {
		t.Errorf("synchronized tags = %+v, want clock_source=ntpd", synchronized.Tags)
	}

	if _, ok := pointByName(points, "system_clock_offset"); ok {
		t.Fatal("system_clock_offset must be omitted when unsynchronised")
	}

	maxError, ok := pointByName(points, "system_clock_max_error")
	if !ok {
		t.Fatalf("system_clock_max_error missing from %+v", points)
	}
	if maxError.Value != 2.0 {
		t.Errorf("max_error = %v, want 2.0 s", maxError.Value)
	}
	if !hasTag(maxError.Tags, "clock_source", "ntpd") {
		t.Errorf("max_error tags = %+v, want clock_source=ntpd", maxError.Tags)
	}
}
