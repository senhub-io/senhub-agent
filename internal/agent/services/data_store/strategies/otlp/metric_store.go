package otlp

import (
	"sort"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// metricStore is the OTLP strategy's last-writer-wins (LWW) cache for
// the most recent value of every (probe, metric, tag-set) series. Each
// AddDataPoints call upserts; the periodic push goroutine snapshots and
// ships the current contents.
//
// Why a strategy-local store rather than reading from the http
// strategy's MetricCache: the OTLP strategy must not depend on http
// being configured. Operators may want OTLP-only deployments. Each
// strategy receives its own copy of every datapoint via the data_store
// router, so each can keep whatever shape of state it needs.
//
// Memory bound: one entry per distinct series. Cardinality is governed
// by:
//   - Probe YAML mappings (the otelmapper package collapses tag values
//     via the `_unmatched` fallback for `expand` mismatches).
//   - `maxEntries` cap (operator-tunable via `otlp.max_store_size`):
//     once the store reaches this many series, new series are dropped
//     and `senhub_agent_otlp_dropped_total{reason="store_cap"}` is
//     incremented. Updates of existing series never drop. 0 means
//     unbounded (the historical behaviour).
type metricStore struct {
	mu         sync.RWMutex
	entries    map[string]storedMetric
	maxEntries int            // 0 = unbounded
	memGuard   *memoryLimiter // optional; nil = no memory limiter
}

// storedMetric is one LWW slot — the metadata we need to feed
// otelmapper.Resolve plus the wall-clock time of the most recent
// observation (becomes the OTLP `time_unix_nano`).
type storedMetric struct {
	probeName  string
	probeType  string
	metricName string
	value      float64
	unit       string
	tags       map[string]string
	observedAt time.Time
}

func newMetricStore() *metricStore {
	return &metricStore{entries: make(map[string]storedMetric)}
}

// newMetricStoreWithCap returns a store that drops new series past
// maxEntries. Pass 0 for unbounded.
func newMetricStoreWithCap(maxEntries int) *metricStore {
	return &metricStore{entries: make(map[string]storedMetric), maxEntries: maxEntries}
}

// withMemoryLimiter attaches a memory limiter to the store. The
// limiter's poll loop must be started separately via memoryLimiter.start.
// Returns the store for chaining.
func (s *metricStore) withMemoryLimiter(ml *memoryLimiter) *metricStore {
	s.memGuard = ml
	return s
}

// upsert records a datapoint, replacing any prior observation for the
// same (probe_name, probe_type, metric_name, tag-set) tuple. Datapoints
// without probe identity are skipped — they cannot be routed through
// otelmapper.Resolve which keys on probe_type to find the YAML.
//
// When the store has reached maxEntries, NEW series are dropped and
// `senhub_agent_otlp_dropped_total{reason="store_cap"}` is incremented.
// Existing series continue to update — preferring continuity of known
// series over admitting unbounded new cardinality, which is the
// expected operator preference when a probe goes rogue on a label.
func (s *metricStore) upsert(dp datapoint.DataPoint) {
	tagMap := flattenTags(dp.Tags)
	probeName := tagMap["probe_name"]
	probeType := tagMap["probe_type"]
	if probeName == "" || probeType == "" {
		return
	}

	key := storeKey(probeName, probeType, dp.Name, tagMap)

	when := dp.Timestamp
	if when.IsZero() {
		when = time.Now()
	}

	// Copy tags into a fresh map so later mutations to the source
	// can't reach inside our store. Cheap relative to the upsert path.
	tagsCopy := make(map[string]string, len(tagMap))
	for k, v := range tagMap {
		tagsCopy[k] = v
	}

	// Memory-pressure check FIRST — checked before the lock so it
	// doesn't contend with the read path under pressure. Lock-free
	// atomic load of the state flag set by the background poller.
	if s.memGuard != nil {
		switch s.memGuard.currentState() {
		case memoryHard:
			// Hard limit: drop everything to give the runtime a chance
			// to GC and recover. Drops are counted by reason so the
			// operator can see they are happening.
			agentstate.IncrementOTLPDropped("memory_hard_limit")
			return
		case memorySoft:
			// Soft limit: keep updating existing series, refuse new
			// series. This is consistent with the cardinality-cap
			// policy (preserve continuity of known series; cut off the
			// runaway-cardinality probe). The drop is recorded only if
			// we actually skip the upsert below.
			s.mu.RLock()
			_, exists := s.entries[key]
			s.mu.RUnlock()
			if !exists {
				agentstate.IncrementOTLPDropped("memory_soft_limit")
				return
			}
		}
	}

	s.mu.Lock()
	if s.maxEntries > 0 {
		if _, exists := s.entries[key]; !exists && len(s.entries) >= s.maxEntries {
			s.mu.Unlock()
			agentstate.IncrementOTLPDropped("store_cap")
			return
		}
	}
	s.entries[key] = storedMetric{
		probeName:  probeName,
		probeType:  probeType,
		metricName: dp.Name,
		value:      float64(dp.Value),
		unit:       tagMap["unit"],
		tags:       tagsCopy,
		observedAt: when,
	}
	s.mu.Unlock()
}

// snapshot returns a slice of CacheMetric ready to feed into
// otelmapper.Resolve, plus the per-series observedAt time aligned by
// index. Callers must not retain references — the maps inside are
// snapshots and may be reused on the next call.
func (s *metricStore) snapshot() ([]otelmapper.CacheMetric, []time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cms := make([]otelmapper.CacheMetric, 0, len(s.entries))
	times := make([]time.Time, 0, len(s.entries))
	for _, e := range s.entries {
		cms = append(cms, otelmapper.CacheMetric{
			ProbeName:  e.probeName,
			ProbeType:  e.probeType,
			MetricName: e.metricName,
			Value:      e.value,
			Unit:       e.unit,
			Tags:       e.tags,
		})
		times = append(times, e.observedAt)
	}
	return cms, times
}

// size reports the current number of stored series; used by tests and
// self-observability.
func (s *metricStore) size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// flattenTags converts the datapoint's tag list into a map. Later tags
// with the same key overwrite earlier ones — same precedence as the
// existing http strategy's MetricCache.
func flattenTags(tagList []tags.Tag) map[string]string {
	out := make(map[string]string, len(tagList))
	for _, t := range tagList {
		out[t.Key] = t.Value
	}
	return out
}

// storeKey produces a stable, unique string for a (probe_name,
// probe_type, metric_name, tags) tuple. Built by sorting tag keys to
// make the key deterministic regardless of tag-list ordering.
func storeKey(probeName, probeType, metricName string, tagMap map[string]string) string {
	keys := make([]string, 0, len(tagMap))
	for k := range tagMap {
		// Skip the systematic identity tags — they're already in the
		// fixed prefix. Keeping them in the suffix would be redundant
		// and just bloats the key.
		if k == "probe_name" || k == "probe_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.Grow(len(probeName) + len(probeType) + len(metricName) + 32)
	b.WriteString(probeName)
	b.WriteByte('|')
	b.WriteString(probeType)
	b.WriteByte('|')
	b.WriteString(metricName)
	for _, k := range keys {
		b.WriteByte('|')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(tagMap[k])
	}
	return b.String()
}

// coerceToFloat64 mirrors the http strategy helper: we may receive
// values typed-opaque on the cache route, but here on AddDataPoints the
// value comes as float32 from datapoint.DataPoint. Kept here so future
// non-numeric extensions have one obvious place to extend.
func coerceToFloat64(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}
