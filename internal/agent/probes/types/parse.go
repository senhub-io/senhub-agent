package types

import "strconv"

// ParseFloat parses s as a 64-bit float. ok is false for an empty or
// non-numeric string, and for a value out of range; s is not trimmed.
func ParseFloat(s string) (v float64, ok bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
