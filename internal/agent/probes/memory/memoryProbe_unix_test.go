//go:build !windows

package memory

import (
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/mem"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

func newUnixMemoryCollectorForTest(t *testing.T) *unixMemoryCollector {
	t.Helper()
	collector, err := newMemoryCollector(map[string]interface{}{}, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("newMemoryCollector: %v", err)
	}
	u, ok := collector.(*unixMemoryCollector)
	if !ok {
		t.Fatalf("newMemoryCollector returned %T, want *unixMemoryCollector", collector)
	}
	return u
}

func tagsEqual(a, b []tags.Tag) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func dataPointsByName(points []data_store.DataPoint) map[string]float64 {
	byName := make(map[string]float64, len(points))
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}
	return byName
}

func TestNewMemoryCollectorReturnsHostpollCollector(t *testing.T) {
	collector, err := newMemoryCollector(map[string]interface{}{}, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("newMemoryCollector: %v", err)
	}
	if _, ok := collector.(hostpoll.Collector); !ok {
		t.Fatalf("collector type %T does not implement hostpoll.Collector", collector)
	}
	if _, ok := collector.(*unixMemoryCollector); !ok {
		t.Fatalf("collector type %T is not *unixMemoryCollector", collector)
	}
}

func TestUnixMemoryCollectorCloseReturnsNil(t *testing.T) {
	u := newUnixMemoryCollectorForTest(t)
	if err := u.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestUnixMemoryCollectorGetBaseTagsReturnsNonEmpty(t *testing.T) {
	u := newUnixMemoryCollectorForTest(t)
	baseTags, err := u.getBaseTags()
	if err != nil {
		t.Fatalf("getBaseTags: %v", err)
	}
	if len(baseTags) == 0 {
		t.Fatal("getBaseTags() returned no tags")
	}
	for _, tag := range baseTags {
		if tag.Key == "" {
			t.Fatalf("getBaseTags() returned a tag with an empty key: %#v", tag)
		}
	}
}

func TestCollectFirstCallEmitsExpectedMetricNamesOnce(t *testing.T) {
	u := newUnixMemoryCollectorForTest(t)
	timestamp := time.Unix(1710000000, 123456789)

	points, err := u.Collect(timestamp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	wantNames := []string{
		"memory_total",
		"memory_used",
		"memory_free",
		"memory_cached",
		"memory_buffers",
		"memory_used_percent",
		"swap_total",
		"swap_used",
		"swap_free",
		"swap_used_percent",
	}

	if len(points) != len(wantNames) {
		t.Fatalf("len(Collect()) = %d, want %d", len(points), len(wantNames))
	}

	counts := map[string]int{}
	for _, dp := range points {
		counts[dp.Name]++
	}

	for _, name := range wantNames {
		if counts[name] != 1 {
			t.Errorf("metric %s emitted %d times, want 1", name, counts[name])
		}
	}

	if _, ok := counts["memory_available"]; ok {
		t.Error("memory_available emitted on first unix collect")
	}
	if _, ok := counts["memory_page_faults"]; ok {
		t.Error("memory_page_faults emitted on first unix collect")
	}
}

func TestCollectFirstCallDatapointsCarryTimestampAndSameBaseTags(t *testing.T) {
	u := newUnixMemoryCollectorForTest(t)
	timestamp := time.Unix(1710000000, 123456789)

	points, err := u.Collect(timestamp)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	first := points[0]
	for i, dp := range points {
		if !dp.Timestamp.Equal(timestamp) {
			t.Errorf("point %d timestamp = %v, want %v", i, dp.Timestamp, timestamp)
		}
		if !tagsEqual(dp.Tags, first.Tags) {
			t.Errorf("point %d tags = %#v, want %#v", i, dp.Tags, first.Tags)
		}
	}
}

func TestCollectFirstCallPercentagesWithinRangeAndMemoryUsedBounded(t *testing.T) {
	u := newUnixMemoryCollectorForTest(t)
	points, err := u.Collect(time.Now())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := dataPointsByName(points)

	if usedPercent, ok := byName["memory_used_percent"]; !ok {
		t.Fatal("missing memory_used_percent")
	} else if usedPercent < 0 || usedPercent > 100 {
		t.Errorf("memory_used_percent = %v, want in [0,100]", usedPercent)
	}

	if swapPercent, ok := byName["swap_used_percent"]; !ok {
		t.Fatal("missing swap_used_percent")
	} else if swapPercent < 0 || swapPercent > 100 {
		t.Errorf("swap_used_percent = %v, want in [0,100]", swapPercent)
	}

	total, totalOK := byName["memory_total"]
	used, usedOK := byName["memory_used"]
	if !totalOK || !usedOK {
		t.Fatal("missing memory_total or memory_used")
	}
	if used > total {
		t.Errorf("memory_used = %v > memory_total = %v", used, total)
	}
}

func TestCollectPageFaultsFirstReadingStoresStateAndEmitsNothing(t *testing.T) {
	u := newUnixMemoryCollectorForTest(t)
	timestamp := time.Now()
	baseTags := []tags.Tag{{Key: "host", Value: "test-host"}}

	before, err := mem.SwapMemory()
	if err != nil {
		t.Fatalf("SwapMemory before: %v", err)
	}

	var points []data_store.DataPoint
	if err := u.collectPageFaults(&points, timestamp, baseTags); err != nil {
		t.Fatalf("collectPageFaults: %v", err)
	}

	after, err := mem.SwapMemory()
	if err != nil {
		t.Fatalf("SwapMemory after: %v", err)
	}

	if len(points) != 0 {
		t.Fatalf("first reading emitted %d points, want 0", len(points))
	}
	if !u.haveFaults {
		t.Error("haveFaults = false after first reading, want true")
	}
	if !u.lastFaultsAt.Equal(timestamp) {
		t.Errorf("lastFaultsAt = %v, want %v", u.lastFaultsAt, timestamp)
	}

	lower := float64(before.PgFault)
	upper := float64(after.PgFault)
	if u.lastFaults < lower || u.lastFaults > upper {
		t.Errorf("lastFaults = %v, want in [%v, %v]", u.lastFaults, lower, upper)
	}
}

func TestCollectPageFaultsSecondReadingEmitsRateFromCounterDelta(t *testing.T) {
	u := newUnixMemoryCollectorForTest(t)
	timestamp := time.Now()
	baseTags := []tags.Tag{{Key: "host", Value: "test-host"}}

	before, err := mem.SwapMemory()
	if err != nil {
		t.Fatalf("SwapMemory before: %v", err)
	}

	u.lastFaults = 0
	u.lastFaultsAt = timestamp.Add(-time.Second)
	u.haveFaults = true

	var points []data_store.DataPoint
	if err := u.collectPageFaults(&points, timestamp, baseTags); err != nil {
		t.Fatalf("collectPageFaults: %v", err)
	}

	after, err := mem.SwapMemory()
	if err != nil {
		t.Fatalf("SwapMemory after: %v", err)
	}

	if len(points) != 1 {
		t.Fatalf("collectPageFaults emitted %d points, want 1", len(points))
	}

	dp := points[0]
	if dp.Name != "memory_page_faults" {
		t.Errorf("point name = %q, want memory_page_faults", dp.Name)
	}
	if !dp.Timestamp.Equal(timestamp) {
		t.Errorf("point timestamp = %v, want %v", dp.Timestamp, timestamp)
	}
	if !tagsEqual(dp.Tags, baseTags) {
		t.Errorf("point tags = %#v, want %#v", dp.Tags, baseTags)
	}

	lower := float64(before.PgFault)
	upper := float64(after.PgFault)
	if dp.Value < lower || dp.Value > upper {
		t.Errorf("memory_page_faults value = %v, want in [%v, %v]", dp.Value, lower, upper)
	}
}

func TestCollectPageFaultsCounterResetEmitsNothingAndUpdatesState(t *testing.T) {
	u := newUnixMemoryCollectorForTest(t)
	timestamp := time.Now()
	baseTags := []tags.Tag{{Key: "host", Value: "test-host"}}

	before, err := mem.SwapMemory()
	if err != nil {
		t.Fatalf("SwapMemory before: %v", err)
	}

	u.lastFaults = float64(^uint64(0))
	u.lastFaultsAt = timestamp.Add(-time.Second)
	u.haveFaults = true

	var points []data_store.DataPoint
	if err := u.collectPageFaults(&points, timestamp, baseTags); err != nil {
		t.Fatalf("collectPageFaults: %v", err)
	}

	after, err := mem.SwapMemory()
	if err != nil {
		t.Fatalf("SwapMemory after: %v", err)
	}

	if len(points) != 0 {
		t.Fatalf("counter reset emitted %d points, want 0", len(points))
	}
	if !u.haveFaults {
		t.Error("haveFaults = false after reset, want true")
	}
	if !u.lastFaultsAt.Equal(timestamp) {
		t.Errorf("lastFaultsAt = %v, want %v", u.lastFaultsAt, timestamp)
	}

	lower := float64(before.PgFault)
	upper := float64(after.PgFault)
	if u.lastFaults < lower || u.lastFaults > upper {
		t.Errorf("lastFaults = %v, want in [%v, %v]", u.lastFaults, lower, upper)
	}
}

func TestCollectPageFaultsNonPositiveElapsedEmitsNothing(t *testing.T) {
	cases := []struct {
		name   string
		prevAt func(time.Time) time.Time
	}{
		{
			name:   "zero elapsed",
			prevAt: func(ts time.Time) time.Time { return ts },
		},
		{
			name:   "negative elapsed",
			prevAt: func(ts time.Time) time.Time { return ts.Add(time.Second) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnixMemoryCollectorForTest(t)
			timestamp := time.Now()
			baseTags := []tags.Tag{{Key: "host", Value: "test-host"}}

			u.lastFaults = 0
			u.lastFaultsAt = tc.prevAt(timestamp)
			u.haveFaults = true

			var points []data_store.DataPoint
			if err := u.collectPageFaults(&points, timestamp, baseTags); err != nil {
				t.Fatalf("collectPageFaults: %v", err)
			}
			if len(points) != 0 {
				t.Fatalf("collectPageFaults emitted %d points, want 0", len(points))
			}
		})
	}
}
