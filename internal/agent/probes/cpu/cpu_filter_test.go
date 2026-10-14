package cpu

import "testing"

func TestShouldCollectCPUCounter(t *testing.T) {
	tests := []struct {
		name           string
		metric         string
		instance       string
		perCoreDetails bool
		want           bool
	}{
		{"per-core usage always kept", "processor_time", "0", false, true},
		{"per-core user skipped by default", "user_time", "0", false, false},
		{"per-core system skipped by default", "privileged_time", "0", false, false},
		{"per-core irq skipped by default", "interrupt_time", "0", false, false},
		{"per-core softirq skipped by default", "dpc_time", "0", false, false},
		{"total user kept", "user_time", "_Total", false, true},
		{"total system kept", "privileged_time", "_Total", false, true},
		{"total irq kept", "interrupt_time", "_Total", false, true},
		{"total softirq kept", "dpc_time", "_Total", false, true},
		{"per-core user restored", "user_time", "0", true, true},
		{"per-core system restored", "privileged_time", "0", true, true},
		{"per-core irq restored", "interrupt_time", "0", true, true},
		{"per-core softirq restored", "dpc_time", "0", true, true},
		{"per-core system counter not a breakdown", "dpc_rate", "0", true, false},
		{"per-core interrupts counter not a breakdown", "interrupt_sec", "0", true, false},
		{"global counter with empty instance kept", "processor_queue_length", "", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCollectCPUCounter(tt.metric, tt.instance, tt.perCoreDetails); got != tt.want {
				t.Fatalf("shouldCollectCPUCounter(%q, %q, %v) = %v, want %v", tt.metric, tt.instance, tt.perCoreDetails, got, tt.want)
			}
		})
	}
}
