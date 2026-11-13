package zookeeper

import "testing"

// Counters such as bytes received pass 2^24 quickly; a 32-bit parse
// rounded them to about seven significant digits, so rates computed
// from successive reads jumped in steps.
func TestNumericStatusParsesAt64Bits(t *testing.T) {
	v, err := parseFloat("123456789012")
	if err != nil || v != 123456789012 {
		t.Fatalf("parsed 123456789012 as %v", v)
	}
}
