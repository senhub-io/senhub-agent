//go:build windows

package cpu

import (
	"testing"
	"time"
)

func TestClockSyncFromCounters(t *testing.T) {
	tests := []struct {
		name         string
		offsetMicros float64
		sourceCount  float64
		wantSynced   bool
		wantOffset   time.Duration
	}{
		{
			name:         "synced",
			offsetMicros: 1234,
			sourceCount:  2,
			wantSynced:   true,
			wantOffset:   1234 * time.Microsecond,
		},
		{
			name:         "unsynced zero sources",
			offsetMicros: 1234,
			sourceCount:  0,
			wantSynced:   false,
			wantOffset:   1234 * time.Microsecond,
		},
		{
			name:         "negative offset",
			offsetMicros: -5678,
			sourceCount:  1,
			wantSynced:   true,
			wantOffset:   -5678 * time.Microsecond,
		},
		{
			name:         "large offset",
			offsetMicros: 1_500_000,
			sourceCount:  3,
			wantSynced:   true,
			wantOffset:   1_500_000 * time.Microsecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := clockSyncFromCounters(tt.offsetMicros, tt.sourceCount)
			if err != nil {
				t.Fatalf("clockSyncFromCounters() error = %v", err)
			}
			if got.synced != tt.wantSynced {
				t.Fatalf("synced = %v, want %v", got.synced, tt.wantSynced)
			}
			if got.offset != tt.wantOffset {
				t.Fatalf("offset = %v, want %v", got.offset, tt.wantOffset)
			}
			if got.source != "w32time" {
				t.Fatalf("source = %q, want %q", got.source, "w32time")
			}
			if !got.maxErrorUnknown {
				t.Fatalf("maxErrorUnknown = false, want true")
			}
		})
	}
}
