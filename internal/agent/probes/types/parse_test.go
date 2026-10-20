package types

import (
	"math"
	"testing"
)

func TestParseFloat(t *testing.T) {
	tests := []struct {
		in     string
		want   float64
		wantOK bool
	}{
		{"3.5", 3.5, true},
		{"-2", -2, true},
		{"1e3", 1000, true},
		{"0.1", 0.1, true},
		{"", 0, false},
		{" 5", 0, false},
		{"abc", 0, false},
		{"1,5", 0, false},
		{"12%", 0, false},
		{"1e999", 0, false},
	}
	for _, tc := range tests {
		got, ok := ParseFloat(tc.in)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("ParseFloat(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
	if got, ok := ParseFloat("NaN"); !ok || !math.IsNaN(got) {
		t.Errorf("ParseFloat(NaN) = %v, %v; want NaN, true", got, ok)
	}
}
