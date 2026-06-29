//go:build windows

package http

// emittedChannelsByProbe is the set of channel names each FREE-wedge
// probe actually emits on this platform — the runtime ground truth, NOT
// the platform-agnostic transformer definition (which is the union of
// every OS and would mask a channel that only one OS produces).
//
// It mirrors the *_windows.go collectors of each probe:
//   - cpu:         internal/agent/probes/cpu/cpuProbe_windows.go
//   - memory:      internal/agent/probes/memory/memoryProbe_windows.go
//   - network:     internal/agent/probes/network/networkProbe_windows.go
//   - logicaldisk: internal/agent/probes/logicaldisk/logicaldiskProbe_windows.go
//
// Keep it in sync when a collector gains or drops a channel. The
// TestDefaultNagiosConfig_ResolvesAgainstEmittedChannels guard exists so
// a default check that references a channel absent from this set — the
// #335 class of regression (Unix-only memory_free/swap_used_percent,
// macOS-only interface names) — fails on the CI platform.
//
// Notably absent here: cpu_load1/5/15 (no load average on Windows) and
// the Unix fs_* disk channels (Windows emits disk_* instead).
var emittedChannelsByProbe = map[string][]string{
	"cpu": {
		"cpu_usage_total", "cpu_user", "cpu_system",
		"cpu_irq", "cpu_softirq",
		"cpu_interrupts", "cpu_dpc_queued", "cpu_dpc_rate",
		"cpu_queue_length",
		"cpu_core_usage", "cpu_core_user", "cpu_core_system",
		"cpu_core_irq", "cpu_core_softirq",
		"cpu_processes_total",
	},
	"memory": {
		"memory_total", "memory_available", "memory_committed",
		"memory_cache", "memory_modified_page_list",
		"memory_nonpaged_pool", "memory_paged_pool",
		"memory_page_faults", "memory_pages_input",
		"memory_pages_output", "memory_used_percent",
	},
	"network": {
		"bytes_sent", "bytes_received",
		"packets_sent", "packets_received",
		"errors_sent", "errors_received",
		"interface_count",
	},
	"logicaldisk": {
		"disk_free_mb", "disk_free_percent", "disk_used_percent",
		"disk_queue_length",
		"disk_read_bytes_sec", "disk_reads_sec",
		"disk_write_bytes_sec", "disk_writes_sec",
	},
}
