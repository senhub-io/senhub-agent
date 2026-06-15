// Package sanitize is the public mirror of the agent's OTel-safe value
// conversion helpers (senhub-agent.go/internal/agent/utils/sanitize).
package sanitize

import (
	"time"

	isanitize "senhub-agent.go/internal/agent/utils/sanitize"
)

// MaxInt32 is the largest int value representable as a float32 without
// overflow; counters above it are capped by CountInt32.
const MaxInt32 = isanitize.MaxInt32

// Duration converts an elapsed time into seconds as a float32, reporting
// false when the input is unusable.
func Duration(t *time.Time, now time.Time) (float32, bool) {
	return isanitize.Duration(t, now)
}

// CountInt32 converts an int64 counter into a float32, reporting false on
// overflow or a non-finite result.
func CountInt32(v int64) (float32, bool) {
	return isanitize.CountInt32(v)
}

// Bytes converts an int64 byte count into a float32, reporting false on
// overflow or a non-finite result.
func Bytes(v int64) (float32, bool) {
	return isanitize.Bytes(v)
}

// EnumValue maps a named enum value to its numeric float32 representation.
func EnumValue(name string, mapping map[string]float32) (float32, bool) {
	return isanitize.EnumValue(name, mapping)
}

// IsFinite reports whether v is neither NaN nor an infinity.
func IsFinite(v float32) bool {
	return isanitize.IsFinite(v)
}
