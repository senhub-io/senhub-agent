//go:build !windows

package network

import "testing"

func TestCounterRate(t *testing.T) {
	tests := []struct {
		name     string
		cur      uint64
		prev     uint64
		seconds  float64
		wantRate float64
		wantOK   bool
	}{
		{"normal", 3000, 1000, 2, 1000, true},
		{"counter reset", 500, 1000, 2, 0, false},
		{"no traffic", 1000, 1000, 2, 0, true},
		{"zero interval", 2000, 1000, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, ok := counterRate(tt.cur, tt.prev, tt.seconds)
			if ok != tt.wantOK {
				t.Errorf("counterRate(%d, %d, %v) ok = %v, want %v", tt.cur, tt.prev, tt.seconds, ok, tt.wantOK)
			}
			if tt.wantOK && rate != tt.wantRate {
				t.Errorf("counterRate(%d, %d, %v) rate = %v, want %v", tt.cur, tt.prev, tt.seconds, rate, tt.wantRate)
			}
		})
	}
}
