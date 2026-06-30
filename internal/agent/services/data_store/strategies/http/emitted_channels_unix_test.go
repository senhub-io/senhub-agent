//go:build !windows

package http

// emittedChannelsByProbe is the set of channel names each FREE-wedge
// probe actually emits on this platform — the runtime ground truth, NOT
// the platform-agnostic transformer definition (which is the union of
// every OS and would mask a channel that only one OS produces).
//
// It mirrors the *_unix.go collectors of each probe:
//   - cpu:         internal/agent/probes/cpu/cpuProbe_unix.go
//   - memory:      internal/agent/probes/memory/memoryProbe_unix.go
//   - network:     internal/agent/probes/network/networkProbe_unix.go
//   - logicaldisk: internal/agent/probes/logicaldisk/logicaldiskProbe_unix.go
//
// Keep it in sync when a collector gains or drops a channel. The
// TestDefaultNagiosConfig_ResolvesAgainstEmittedChannels guard exists so
// a default check that references a channel absent from this set — the
// #335 class of regression (macOS-only "utun4"/"en3" interfaces,
// Windows-only memory_available) — fails on the CI platform.
var emittedChannelsByProbe = map[string][]string{
	"cpu": {
		"cpu_usage_total",
		"cpu_user", "cpu_system", "cpu_idle", "cpu_nice",
		"cpu_iowait", "cpu_irq", "cpu_softirq", "cpu_steal",
		"cpu_load1", "cpu_load5", "cpu_load15",
		"cpu_core_usage",
		"cpu_processes_total",
	},
	"memory": {
		"memory_total", "memory_used", "memory_free",
		"memory_cached", "memory_buffers", "memory_used_percent",
		"swap_total", "swap_used", "swap_free", "swap_used_percent",
	},
	"network": {
		"bytes_sent", "bytes_received",
		"packets_sent", "packets_received",
		"errors_sent", "errors_received",
	},
	"logicaldisk": {
		"fs_total_bytes", "fs_used_bytes", "fs_free_bytes",
		"fs_available_bytes", "fs_used_percent",
		"fs_inodes_total", "fs_inodes_used", "fs_inodes_free",
		"fs_inodes_used_percent",
	},
}
