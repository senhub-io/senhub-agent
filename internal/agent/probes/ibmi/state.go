package ibmi

import (
	"sync"
	"time"

	"senhub-agent.go/probesdk/datapoint"
)

// deltaStore keeps the last observed numeric value for every
// (metric_name, tag_set) pair and derives both an absolute delta and
// a per-second rate on the subsequent observation. It is a tiny piece
// of probe-wide cross-cycle state: individual collectors can opt-in
// by handing their raw cumulative DataPoints to Derive() before
// returning them to the probe.
//
// The raw DataPoint and the derived "<name>_delta" / "<name>_rate_per_sec"
// are both emitted, so downstream dashboards can pick whichever they
// need. Counters that reset (e.g. job restart) produce a negative
// delta which we clamp to zero to avoid lying about rates — negative
// rates are almost always a symptom of measurement resets and rarely
// something an operator wants plotted.
type deltaStore struct {
	mu sync.Mutex
	// key = metric_name + "|" + tagsString(tags)
	lastValue map[string]float64
	lastSeen  map[string]time.Time
}

// newDeltaStore returns an empty delta store. It is safe for
// concurrent use.
func newDeltaStore() *deltaStore {
	return &deltaStore{
		lastValue: make(map[string]float64),
		lastSeen:  make(map[string]time.Time),
	}
}

// Derive takes a cumulative DataPoint, records its current value, and
// returns zero or two additional DataPoints representing the absolute
// delta and the per-second rate since the previous observation of the
// same series. On the first observation (nothing in state yet) it
// returns nil — you need two samples to compute a derivative.
//
// The rate DataPoint uses the same tag set as the input and appends no
// extras; the metric name is derived from the input name with
// "_delta" / "_rate_per_sec" suffixes so Prometheus unit conventions
// still apply.
func (d *deltaStore) Derive(dp datapoint.DataPoint) []datapoint.DataPoint {
	key := dp.Name + "|" + tagsString(dp.Tags)
	current := float64(dp.Value)

	d.mu.Lock()
	prev, existed := d.lastValue[key]
	prevTs := d.lastSeen[key]
	d.lastValue[key] = current
	d.lastSeen[key] = dp.Timestamp
	d.mu.Unlock()

	if !existed {
		return nil
	}

	diff := current - prev
	if diff < 0 {
		// Counter reset (job restart, machine reboot) — emit a zero
		// delta and skip the rate, rather than a misleading spike.
		diff = 0
	}
	elapsed := dp.Timestamp.Sub(prevTs).Seconds()
	if elapsed <= 0 {
		elapsed = 1 // defensive: guard against timestamps going
		// backwards, which would otherwise produce an infinite rate
	}
	rate := diff / elapsed

	return []datapoint.DataPoint{
		{
			Name:      dp.Name + "_delta",
			Timestamp: dp.Timestamp,
			Value:     float32(diff),
			Tags:      dp.Tags,
		},
		{
			Name:      dp.Name + "_rate_per_sec",
			Timestamp: dp.Timestamp,
			Value:     float32(rate),
			Tags:      dp.Tags,
		},
	}
}
