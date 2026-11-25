package probes

import (
	"hash/fnv"
	"time"
)

// maxStartSpread caps how far into its interval a probe's first collection
// may be pushed, so a probe with a long interval still delivers its first
// data within half a minute of the start.
const maxStartSpread = 30 * time.Second

// StartOffset is the phase a probe keeps between its collections: the
// FNV-1a 64 hash of its id modulo min(interval, maxStartSpread). The first
// collection runs at once; the scheduler then waits this offset before its
// ticker starts, so the second collection comes one interval plus the
// offset after the first. The same probe gets the same offset on every
// start, so the timing PRTG and the other readers see does not move
// between restarts. A probe without an interval is not delayed.
func StartOffset(probeID string, interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	bound := min(interval, maxStartSpread)
	h := fnv.New64a()
	_, _ = h.Write([]byte(probeID))
	return time.Duration(h.Sum64() % uint64(bound))
}
