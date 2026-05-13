// Package sanitize provides small, well-defined helpers that probes use to
// turn raw external values (Veeam API JSON, perfmon counters, gopsutil
// uint64s, …) into the float32 metric values the cache stores.
//
// The agent has been bitten more than once by garbage flowing all the way
// to PRTG / Prometheus because a probe was casting a uint64 directly to
// float32, computing now-LastRun without checking for the Go zero time, or
// switch-defaulting an unknown enum to 0 silently. The helpers here aim
// to make the "safe" path the obvious path:
//
//   - zero-time and nil-pointer time are both treated as "no data".
//   - Future timestamps (clock skew) are clamped to zero.
//   - Counts larger than what a 32-bit consumer (PRTG) can serialize
//     come back with ok=false so callers can choose to drop the
//     metric rather than send -2147483648 noise.
//   - NaN and +/-Inf are never propagated.
//
// All helpers return `(value, ok)` so the caller decides what to do — emit
// the value, skip the metric, or substitute a sentinel. This package does
// NOT log anything; logging happens at the call site so the context
// (probe_name, metric_name, raw input) can be attached.
package sanitize

import (
	"math"
	"time"
)

// MaxInt32 mirrors math.MaxInt32 as a float32 sentinel — the largest
// integer value PRTG / Nagios 32-bit consumers can render without overflow.
// Stored here as a constant so probe code can clamp without depending on
// math directly.
const MaxInt32 float32 = 2147483647

// Duration returns the wall-clock seconds between `t` and `now`, treating
// nil pointers and the Go zero time as "no data". Future timestamps —
// caused by clock skew between the agent host and a monitored system —
// clamp to zero rather than producing a negative duration.
//
// Returns (seconds, true) when the input is usable, (0, false) otherwise.
// The caller decides whether to emit a sentinel (e.g. -1 for "never run")
// or drop the metric entirely.
func Duration(t *time.Time, now time.Time) (float32, bool) {
	if t == nil || t.IsZero() {
		return 0, false
	}
	d := now.Sub(*t).Seconds()
	if d < 0 {
		// Future timestamp — treat as 0 rather than reporting a
		// negative age which downstream dashboards cannot represent.
		return 0, true
	}
	if d > float64(math.MaxFloat32) {
		// Pathological — but be defensive in case a 0001-01-01
		// snuck past the IsZero check (e.g. the API parsed a
		// 1970-epoch as 0001-something).
		return 0, false
	}
	return float32(d), true
}

// CountInt32 converts an int64 count (number of objects, packets, …) into
// a PRTG-safe float32. Negative inputs and values that exceed MaxInt32
// are reported as not-ok so the caller can skip or warn — silently
// clamping would hide upstream API bugs.
func CountInt32(v int64) (float32, bool) {
	if v < 0 {
		return 0, false
	}
	if v > math.MaxInt32 {
		return MaxInt32, false
	}
	return float32(v), true
}

// BytesInt32 is identical to CountInt32 today but kept as a distinct
// entry point so future tuning (e.g. clamping at petabyte scale rather
// than MaxInt32) can target byte-typed metrics specifically without
// affecting object counts.
func BytesInt32(v int64) (float32, bool) {
	return CountInt32(v)
}

// EnumValue looks `name` up in `mapping`. The mapping should be a small,
// case-insensitive table of API string values to numeric codes (e.g.
// {"none": 0, "source": 1, ...}). Returns (code, true) on a hit. On a
// miss, (0, false) — callers should NOT emit a default 0 silently
// because that loses the signal that the API returned something new.
//
// The lookup is case-insensitive on the key side to absorb API
// inconsistencies (Veeam used to return "source" lowercase on some
// endpoints and "Source" on others between v11 and v12).
func EnumValue(name string, mapping map[string]float32) (float32, bool) {
	if name == "" {
		return 0, false
	}
	if v, ok := mapping[name]; ok {
		return v, true
	}
	// Case-insensitive retry. Cheap because mappings here are tiny
	// (typically <10 entries).
	for k, v := range mapping {
		if equalFoldShort(k, name) {
			return v, true
		}
	}
	return 0, false
}

// IsFinite reports whether v is a real number (not NaN, not +/-Inf). A
// false return means the caller should drop the value rather than push
// it into the cache.
func IsFinite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// equalFoldShort is a tiny ASCII case-insensitive compare. We deliberately
// avoid strings.EqualFold here because that pulls in the full Unicode
// case-folding machinery; enum keys are always ASCII identifiers.
func equalFoldShort(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
