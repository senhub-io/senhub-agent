package otlp

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func TestMetricStore_LWW(t *testing.T) {
	store := newMetricStore()
	identity := []tags.Tag{
		{Key: "probe_name", Value: "host-a"},
		{Key: "probe_type", Value: "cpu"},
	}

	store.upsert(datapoint.DataPoint{Name: "cpu.usage", Value: 1.0, Tags: identity})
	store.upsert(datapoint.DataPoint{Name: "cpu.usage", Value: 2.0, Tags: identity})

	cms, _ := store.snapshot()
	if len(cms) != 1 {
		t.Fatalf("LWW collapse failed: got %d entries", len(cms))
	}
	if cms[0].Value != 2.0 {
		t.Errorf("LWW value=%v, want 2.0", cms[0].Value)
	}
}

func TestMetricStore_KeyDistinctOnTags(t *testing.T) {
	store := newMetricStore()
	base := []tags.Tag{
		{Key: "probe_name", Value: "host-a"},
		{Key: "probe_type", Value: "cpu"},
	}
	store.upsert(datapoint.DataPoint{
		Name:  "cpu.time",
		Value: 1.0,
		Tags:  append(base, tags.Tag{Key: "core", Value: "0"}),
	})
	store.upsert(datapoint.DataPoint{
		Name:  "cpu.time",
		Value: 2.0,
		Tags:  append(base, tags.Tag{Key: "core", Value: "1"}),
	})

	if got := store.size(); got != 2 {
		t.Errorf("size=%d, want 2 (different tag sets)", got)
	}
}

func TestMetricStore_DropsOrphans(t *testing.T) {
	store := newMetricStore()

	// No tags at all.
	store.upsert(datapoint.DataPoint{Name: "x", Value: 1})
	// Has probe_type but no probe_name.
	store.upsert(datapoint.DataPoint{
		Name:  "y",
		Value: 1,
		Tags:  []tags.Tag{{Key: "probe_type", Value: "cpu"}},
	})

	if got := store.size(); got != 0 {
		t.Errorf("orphan dps stored: size=%d", got)
	}
}

func TestMetricStore_DefaultsTimestamp(t *testing.T) {
	store := newMetricStore()
	identity := []tags.Tag{
		{Key: "probe_name", Value: "p"},
		{Key: "probe_type", Value: "t"},
	}

	before := time.Now()
	store.upsert(datapoint.DataPoint{Name: "m", Value: 1, Tags: identity})
	after := time.Now()

	_, times := store.snapshot()
	if len(times) != 1 {
		t.Fatalf("times=%v", times)
	}
	if times[0].Before(before) || times[0].After(after) {
		t.Errorf("default timestamp out of range: %v not in [%v, %v]", times[0], before, after)
	}
}

func TestMetricStore_TagsCopied(t *testing.T) {
	// After upsert, mutating the source DataPoint's tag slice must not
	// affect the stored entry.
	store := newMetricStore()
	srcTags := []tags.Tag{
		{Key: "probe_name", Value: "p"},
		{Key: "probe_type", Value: "t"},
		{Key: "host", Value: "before"},
	}
	store.upsert(datapoint.DataPoint{Name: "m", Value: 1, Tags: srcTags})

	srcTags[2].Value = "after"

	cms, _ := store.snapshot()
	if cms[0].Tags["host"] != "before" {
		t.Errorf("stored tag mutated externally: got %q", cms[0].Tags["host"])
	}
}

func TestStoreKey_DeterministicIgnoringOrder(t *testing.T) {
	a := storeKey("p", "t", "m", map[string]string{"x": "1", "y": "2"})
	b := storeKey("p", "t", "m", map[string]string{"y": "2", "x": "1"})
	if a != b {
		t.Errorf("key differs by tag order: %q vs %q", a, b)
	}
}

func TestStoreKey_IgnoresIdentityTags(t *testing.T) {
	// probe_name / probe_type are part of the fixed prefix; including
	// them in the suffix would make the key redundant.
	a := storeKey("p", "t", "m", map[string]string{"x": "1"})
	b := storeKey("p", "t", "m", map[string]string{
		"x":          "1",
		"probe_name": "p",
		"probe_type": "t",
	})
	if a != b {
		t.Errorf("identity tags affected suffix: %q vs %q", a, b)
	}
}

func TestMetricStore_CardinalityCap_DropsNewSeries(t *testing.T) {
	store := newMetricStoreWithCap(2)
	mk := func(metric, host string) datapoint.DataPoint {
		return datapoint.DataPoint{
			Name:  metric,
			Value: 1,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "p"},
				{Key: "probe_type", Value: "t"},
				{Key: "host", Value: host},
			},
		}
	}

	store.upsert(mk("a", "h1")) // 1 series
	store.upsert(mk("b", "h1")) // 2 series — at cap
	store.upsert(mk("c", "h1")) // 3rd series — DROPPED

	if got := store.size(); got != 2 {
		t.Errorf("size=%d after cap exhaustion, want 2", got)
	}

	// Existing series must still update.
	store.upsert(mk("a", "h1"))
	if got := store.size(); got != 2 {
		t.Errorf("size=%d after update of existing series, want 2", got)
	}
}

func TestMetricStore_CardinalityCap_ZeroMeansUnbounded(t *testing.T) {
	store := newMetricStoreWithCap(0)
	for i := 0; i < 100; i++ {
		store.upsert(datapoint.DataPoint{
			Name:  "m",
			Value: float64(i),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "p"},
				{Key: "probe_type", Value: "t"},
				{Key: "host", Value: hostOf(i)},
			},
		})
	}
	if got := store.size(); got != 100 {
		t.Errorf("size=%d with cap=0 (unbounded), want 100", got)
	}
}

func hostOf(i int) string { return "h" + itoa(i) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

func TestMetricStore_ProbeBudget_DropsAtBudget(t *testing.T) {
	store := newMetricStoreWithCap(0).withProbeBudget(2)
	mk := func(probe, metric string) datapoint.DataPoint {
		return datapoint.DataPoint{
			Name:  metric,
			Value: 1,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: probe},
				{Key: "probe_type", Value: "t"},
			},
		}
	}

	store.upsert(mk("p1", "a"))
	store.upsert(mk("p1", "b"))
	store.upsert(mk("p1", "c")) // p1 at budget — DROPPED

	if got := store.probeSeriesCount("p1"); got != 2 {
		t.Errorf("p1 count=%d after budget, want 2", got)
	}

	// p2 still has its own budget — admits 2 series of its own
	store.upsert(mk("p2", "a"))
	store.upsert(mk("p2", "b"))
	if got := store.probeSeriesCount("p2"); got != 2 {
		t.Errorf("p2 count=%d, want 2 (isolated budget)", got)
	}

	// updates of existing series don't increment the counter
	store.upsert(mk("p1", "a"))
	if got := store.probeSeriesCount("p1"); got != 2 {
		t.Errorf("p1 count=%d after re-upsert of existing, want 2", got)
	}
}

func TestMetricStore_ProbeBudget_ZeroMeansUnbounded(t *testing.T) {
	store := newMetricStoreWithCap(0).withProbeBudget(0)
	mk := func(metric string) datapoint.DataPoint {
		return datapoint.DataPoint{Name: metric, Value: 1, Tags: []tags.Tag{
			{Key: "probe_name", Value: "p"}, {Key: "probe_type", Value: "t"}}}
	}
	for i := 0; i < 50; i++ {
		store.upsert(mk("m" + itoa(i)))
	}
	if got := store.probeSeriesCount("p"); got != 50 {
		t.Errorf("count=%d with budget=0 (unbounded), want 50", got)
	}
}

func TestMetricStore_ProbeBudget_StacksWithGlobalCap(t *testing.T) {
	// Global cap 3, per-probe budget 5. The smaller (global) wins.
	store := newMetricStoreWithCap(3).withProbeBudget(5)
	mk := func(probe, metric string) datapoint.DataPoint {
		return datapoint.DataPoint{Name: metric, Value: 1, Tags: []tags.Tag{
			{Key: "probe_name", Value: probe}, {Key: "probe_type", Value: "t"}}}
	}
	store.upsert(mk("p1", "a"))
	store.upsert(mk("p1", "b"))
	store.upsert(mk("p2", "a"))
	store.upsert(mk("p2", "b")) // global cap hit at this point (4 > 3)
	if got := store.size(); got != 3 {
		t.Errorf("global cap should win: size=%d, want 3", got)
	}
}

// TestMetricStore_EvictStale pins the #308 fix: entries whose last
// datapoint is older than the TTL are evicted (probe budget slots
// released), fresh entries survive, ttl=0 disables eviction.
func TestMetricStore_EvictStale(t *testing.T) {
	store := newMetricStore()
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	mk := func(probe, metric string, ts time.Time) datapoint.DataPoint {
		return datapoint.DataPoint{
			Name: metric, Value: 1, Timestamp: ts,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: probe},
				{Key: "probe_type", Value: "t"},
			},
		}
	}

	store.upsert(mk("dead-probe", "senhub.ibmi.cpu", now.Add(-time.Hour)))
	store.upsert(mk("live-probe", "system.cpu.utilization", now.Add(-time.Minute)))

	if n := store.evictStale(now, 0); n != 0 {
		t.Fatalf("ttl=0 must disable eviction, evicted %d", n)
	}
	if n := store.evictStale(now, 10*time.Minute); n != 1 {
		t.Fatalf("evicted %d entries, want 1 (the hour-old one)", n)
	}
	if store.size() != 1 {
		t.Fatalf("store size = %d, want 1", store.size())
	}
	metrics, _ := store.snapshot()
	if len(metrics) != 1 || metrics[0].MetricName != "system.cpu.utilization" {
		t.Fatalf("survivor = %+v, want the live series", metrics)
	}

	// The dead probe's budget slot is released: with a budget of 1, a
	// new series for that probe is admitted after eviction.
	s2 := newMetricStoreWithCap(0).withProbeBudget(1)
	s2.upsert(mk("p", "a", now.Add(-time.Hour)))
	s2.evictStale(now, 10*time.Minute)
	s2.upsert(mk("p", "b", now))
	if got := s2.probeSeriesCount("p"); got != 1 {
		t.Fatalf("budget slot not released after eviction: count=%d", got)
	}
	if m2, _ := s2.snapshot(); len(m2) != 1 || m2[0].MetricName != "b" {
		t.Fatalf("post-eviction admit failed: %+v", m2)
	}
}
