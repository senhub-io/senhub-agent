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
//
// A probe that collects less often than the push vouches for its last
// value until its next run is due, so the allowance grows by one and a
// half of its cadence. Without it an hourly probe was visible to the
// server ninety seconds an hour: a new host waited an hour for its first
// value, and the probe's discovery vanished between two runs.
type store struct {
	mu       sync.RWMutex
	entries  map[string]entry
	cadences map[string]time.Duration // probe name -> collection interval
}

type entry struct {
	metric     otelmapper.CacheMetric
	observedAt time.Time
	// receivedAt is the arrival time on the agent's clock, which tells
	// one probe run from the next where a source timestamp cannot.
	receivedAt time.Time
	// retired marks a series a later run of its probe did not report.
	// Its value is no longer pushed, but it stays in the discovery rows
	// until it expires: dropping it there would have Zabbix disable the
	// item on every outage of the target and bring it back afterwards.
	retired bool
}

// Tag keys the store reads; named constants keep the parameter guard from
// reading these tag lookups as configuration keys.
const (
	tagProbeName = "probe_name"
	tagProbeType = "probe_type"
	tagUnit      = "unit"
)

func newStore() *store {
	return &store{entries: map[string]entry{}, cadences: map[string]time.Duration{}}
}

func (s *store) upsert(dp datapoint.DataPoint) {
	s.upsertAt(dp, time.Now())
}

func (s *store) upsertAt(dp datapoint.DataPoint, receivedAt time.Time) {
	tagMap := make(map[string]string, len(dp.Tags))
	for _, t := range dp.Tags {
		if t.Private {
			continue
		}
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
		receivedAt: receivedAt,
	}
	s.mu.Unlock()
}

// retireSuperseded marks, for each probe that just delivered a batch, the
// series a previous run reported and this one did not: a series received
// more than half a cadence before the batch. Without it the last values
// of a target that went down were pushed as current until they expired
// (#951). Batches of one run arrive seconds apart and are kept together.
func (s *store) retireSuperseded(probes map[string]bool, arrival time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.entries {
		if e.retired || !probes[e.metric.ProbeName] {
			continue
		}
		interval := s.cadences[e.metric.ProbeName]
		if interval <= 0 || arrival.Sub(e.receivedAt) <= interval/2 {
			continue
		}
		e.retired = true
		s.entries[k] = e
	}
}

// noteProbeCadence records how often the named probe collects.
func (s *store) noteProbeCadence(probeName string, interval time.Duration) {
	if probeName == "" || interval <= 0 {
		return
	}
	s.mu.Lock()
	s.cadences[probeName] = interval
	s.mu.Unlock()
}

// snapshot returns the series whose value is pushed, and every live
// series for discovery, dropping those older than ttl, or than ttl plus
// one and a half cadences for a probe whose cadence is known.
func (s *store) snapshot(now time.Time, ttl time.Duration) (current, discovered []otelmapper.CacheMetric) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current = make([]otelmapper.CacheMetric, 0, len(s.entries))
	discovered = make([]otelmapper.CacheMetric, 0, len(s.entries))
	for k, e := range s.entries {
		if ttl > 0 && now.Sub(e.observedAt) > ttl+s.cadences[e.metric.ProbeName]*3/2 {
			delete(s.entries, k)
			continue
		}
		discovered = append(discovered, e.metric)
		if !e.retired {
			current = append(current, e.metric)
		}
	}
	return current, discovered
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
