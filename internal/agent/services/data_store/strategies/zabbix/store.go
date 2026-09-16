package zabbix

import (
	"sort"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// store keeps the latest value of every series the probes produced, so a
// push sends the current state of each item rather than every sample
// collected since the last one. A series that stops being collected is
// forgotten after ttl: Zabbix reports the item unsupported or stale on
// its own, which says more than a value re-sent forever.
type store struct {
	mu      sync.RWMutex
	entries map[string]entry
}

type entry struct {
	metric     otelmapper.CacheMetric
	observedAt time.Time
}

// Tag keys the store reads; named constants keep the parameter guard from
// reading these tag lookups as configuration keys.
const (
	tagProbeName = "probe_name"
	tagProbeType = "probe_type"
	tagUnit      = "unit"
)

func newStore() *store {
	return &store{entries: map[string]entry{}}
}

func (s *store) upsert(dp datapoint.DataPoint) {
	tagMap := make(map[string]string, len(dp.Tags))
	for _, t := range dp.Tags {
		tagMap[t.Key] = t.Value
	}
	probeName, probeType := tagMap[tagProbeName], tagMap[tagProbeType]
	if probeName == "" || probeType == "" {
		return
	}
	when := dp.Timestamp
	if when.IsZero() {
		when = time.Now()
	}
	unit := tagMap[tagUnit]

	s.mu.Lock()
	s.entries[storeKey(probeName, probeType, dp.Name, tagMap)] = entry{
		metric: otelmapper.CacheMetric{
			ProbeName:  probeName,
			ProbeType:  probeType,
			MetricName: dp.Name,
			Value:      dp.Value,
			Unit:       unit,
			Tags:       tagMap,
			Histogram:  dp.Histogram,
		},
		observedAt: when,
	}
	s.mu.Unlock()
}

// snapshot returns the live series and drops those older than ttl.
func (s *store) snapshot(now time.Time, ttl time.Duration) []otelmapper.CacheMetric {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]otelmapper.CacheMetric, 0, len(s.entries))
	for k, e := range s.entries {
		if ttl > 0 && now.Sub(e.observedAt) > ttl {
			delete(s.entries, k)
			continue
		}
		out = append(out, e.metric)
	}
	return out
}

func (s *store) size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

func storeKey(probeName, probeType, metric string, tagMap map[string]string) string {
	keys := make([]string, 0, len(tagMap))
	for k := range tagMap {
		if k == tagProbeName || k == tagProbeType || k == tagUnit {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(probeName)
	b.WriteByte(0)
	b.WriteString(probeType)
	b.WriteByte(0)
	b.WriteString(metric)
	for _, k := range keys {
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(tagMap[k])
	}
	return b.String()
}

// tagValue is a small helper for tests and diagnostics.
func tagValue(ts []tags.Tag, key string) string {
	for _, t := range ts {
		if t.Key == key {
			return t.Value
		}
	}
	return ""
}
