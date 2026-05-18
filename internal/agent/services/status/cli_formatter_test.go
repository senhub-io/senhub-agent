package status

import (
	"strings"
	"testing"
)

func TestFormatOTLPInfo_NilReturnsEmpty(t *testing.T) {
	f := NewCLIFormatter()
	if got := f.FormatOTLPInfo(nil); got != "" {
		t.Errorf("FormatOTLPInfo(nil) = %q, want empty string", got)
	}
}

func TestFormatOTLPInfo_RendersAllSections(t *testing.T) {
	info := &OTLPInfo{}
	info.Pipeline.MetricsPushedTotal = 1234
	info.Pipeline.LogsPushedTotal = 56
	info.Pipeline.ExportErrorsTotal = 0
	info.Pipeline.DroppedTotal = 5
	info.Pipeline.DroppedByReason = map[string]uint64{"store_cap": 3, "memory_soft_limit": 2}
	info.Store.Size = 4825
	info.Store.LogBufferFillRatio = 0.42
	info.ExportDuration.LastMs = 16670
	info.ExportDuration.MeanMs = 17440
	info.Checkpoint.SizeBytes = 2048
	info.Checkpoint.LastSaveAgeSeconds = 12
	info.Checkpoint.RestoredEntries = 100
	info.Checkpoint.ErrorsTotal = 1
	info.Checkpoint.ErrorsByStage = map[string]uint64{"fsync": 1}
	info.Parallel.SubBatches = 6

	out := NewCLIFormatter().FormatOTLPInfo(info)

	// One assertion theme: the four section headers are present.
	for _, want := range []string{"OTLP Pipeline", "Checkpoint", "Parallel export"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestFormatOTLPInfo_SeedsPipelineCounters(t *testing.T) {
	info := &OTLPInfo{}
	info.Pipeline.MetricsPushedTotal = 1234
	info.Pipeline.DroppedTotal = 5
	info.Pipeline.DroppedByReason = map[string]uint64{"store_cap": 5}

	out := NewCLIFormatter().FormatOTLPInfo(info)

	for _, want := range []string{"1234", "store_cap", "5"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestFormatOTLPInfo_ParallelPathLabel(t *testing.T) {
	tests := []struct {
		name       string
		subBatches int32
		wantLabel  string
	}{
		{"single batch when sub_batches <= 1", 1, "single-batch path"},
		{"fan-out when sub_batches > 1", 4, "fan-out by probe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &OTLPInfo{}
			info.Parallel.SubBatches = tt.subBatches
			out := NewCLIFormatter().FormatOTLPInfo(info)
			if !strings.Contains(out, tt.wantLabel) {
				t.Errorf("missing %q in output:\n%s", tt.wantLabel, out)
			}
		})
	}
}

func TestFormatOTLPInfo_CheckpointDisabledWhenAllZero(t *testing.T) {
	info := &OTLPInfo{}
	// Leave all checkpoint fields at zero — represents an agent
	// configured without persistence enabled.
	out := NewCLIFormatter().FormatOTLPInfo(info)
	if !strings.Contains(out, "Disabled (no save observed)") {
		t.Errorf("expected disabled checkpoint message, got:\n%s", out)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{2 * 1024 * 1024, "2.0 MiB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatMsDuration(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "—"},
		{-1, "—"},
		{42, "42 ms"},
		{999, "999 ms"},
		{1000, "1.00 s"},
		{17440, "17.44 s"},
	}
	for _, tt := range tests {
		if got := formatMsDuration(tt.in); got != tt.want {
			t.Errorf("formatMsDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
