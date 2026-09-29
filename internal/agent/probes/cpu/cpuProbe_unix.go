//go:build !windows

// internal/agent/probes/host/cpuProbe_unix.go
package cpu

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type unixCollector struct {
	logger *logger.Logger
	// lastTimes holds the previous cumulative CPU time snapshot used to
	// derive per-mode utilization percentages. cpu.Times() returns
	// cumulative seconds since boot; we keep the previous reading so
	// each Collect() can emit (current - last) normalized over the
	// elapsed wall time as a 0-100 % value per mode.
	lastTimes     *cpu.TimesStat
	lastTimestamp time.Time
	// lastKernel holds the previous reading of the kernel's cumulative
	// counters, which are rates once divided by the elapsed time. The
	// definitions declare them in 1/s, the way the Windows collector
	// already reports them.
	lastKernel   *kernelCounters
	lastKernelAt time.Time
}

func newCPUCollector(config map[string]interface{}, logger *logger.Logger) (hostpoll.Collector, error) {
	return &unixCollector{
		logger: logger,
	}, nil
}

func (u *unixCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	dataPoints := make([]data_store.DataPoint, 0, 20)

	baseTags, err := u.getBaseTags()
	if err != nil {
		return nil, err
	}

	// Try to collect CPU times, but don't fail if not available (e.g., on macOS)
	if err := u.collectCPUTimes(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect CPU times (may not be supported on this OS)")
	}

	// Collect CPU usage percentage (usually works on all platforms)
	if err := u.collectCPUUsage(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect CPU usage percentage")
	}

	// Collect load average (Unix-specific, usually works)
	if err := u.collectLoadAverage(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect load average")
	}

	// Collect per-core metrics
	if err := u.collectPerCoreMetrics(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect per-core metrics")
	}

	// Collect process count (cross-OS metric — needed for the host
	// dashboards' "running processes" panel à la node_exporter)
	if err := u.collectProcessesCount(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect process count")
	}

	// The processor count, which a native Zabbix agent reports as
	// system.cpu.num and every capacity chart divides by.
	if err := u.collectCPUCount(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect the processor count")
	}

	// Interrupts, context switches and runnable processes, which the
	// kernel exposes as cumulative counters. Absent outside Linux.
	if err := u.collectKernelCounters(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Debug().Err(err).Msg("Kernel counters not available on this OS")
	}

	// If we couldn't collect any metrics at all, return an error
	if len(dataPoints) == 0 {
		return nil, fmt.Errorf("failed to collect any CPU metrics")
	}

	return dataPoints, nil
}

func (u *unixCollector) getBaseTags() ([]tags.Tag, error) {
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %w", err)
	}
	return baseTags, nil
}

func (u *unixCollector) collectCPUTimes(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	times, err := cpu.Times(false)
	if err != nil {
		return fmt.Errorf("error getting CPU times: %w", err)
	}

	if len(times) == 0 {
		return nil
	}

	curr := times[0]

	// On the first collection we have nothing to diff against — store the
	// snapshot and return without emitting per-mode percentages. The first
	// real datapoints arrive on the second tick of the probe interval.
	// Total CPU usage (cpu_usage_total) and per-core values come from
	// gopsutil's blocking cpu.Percent path and don't need this warm-up.
	if u.lastTimes == nil {
		u.lastTimes = &curr
		u.lastTimestamp = timestamp
		return nil
	}

	prev := *u.lastTimes
	// Sum of all mode deltas — denominator for the per-mode percentage.
	// We compute it from the times themselves (not wall-clock × NumCPU)
	// so the math matches what `top` / `mpstat` report and stays
	// consistent across single-CPU containers, virtualized environments
	// and physical multi-core hosts.
	// The kernel counts guest time inside user time, and guest nice
	// inside nice, so reporting both without subtracting would count
	// those ticks twice and push every other mode down. mpstat and
	// node_exporter do the same subtraction. A source that has already
	// subtracted them would drive the result negative, so the delta is
	// only taken when it stays positive.
	guestDelta := curr.Guest - prev.Guest
	guestNiceDelta := curr.GuestNice - prev.GuestNice
	userDelta := curr.User - prev.User
	niceDelta := curr.Nice - prev.Nice
	if userDelta-guestDelta >= 0 {
		userDelta -= guestDelta
	}
	if niceDelta-guestNiceDelta >= 0 {
		niceDelta -= guestNiceDelta
	}

	totalDelta := userDelta +
		(curr.System - prev.System) +
		(curr.Idle - prev.Idle) +
		niceDelta +
		(curr.Iowait - prev.Iowait) +
		(curr.Irq - prev.Irq) +
		(curr.Softirq - prev.Softirq) +
		(curr.Steal - prev.Steal) +
		guestDelta +
		guestNiceDelta

	u.lastTimes = &curr
	u.lastTimestamp = timestamp

	if totalDelta <= 0 {
		// Clock skew, hibernate/resume, or a tick interval below kernel
		// granularity — skip this round rather than emit divide-by-zero
		// or negative percentages.
		return nil
	}

	pct := func(delta float64) float64 {
		v := (delta / totalDelta) * 100.0
		if v < 0 {
			return 0
		}
		if v > 100 {
			return 100
		}
		return v
	}

	metrics := []struct {
		name  string
		value float64
	}{
		{"cpu_user", pct(userDelta)},
		{"cpu_system", pct(curr.System - prev.System)},
		{"cpu_idle", pct(curr.Idle - prev.Idle)},
		{"cpu_nice", pct(niceDelta)},
		{"cpu_iowait", pct(curr.Iowait - prev.Iowait)},
		{"cpu_irq", pct(curr.Irq - prev.Irq)},
		{"cpu_softirq", pct(curr.Softirq - prev.Softirq)},
		{"cpu_steal", pct(curr.Steal - prev.Steal)},
		{"cpu_guest", pct(guestDelta)},
		{"cpu_guest_nice", pct(guestNiceDelta)},
	}

	for _, metric := range metrics {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      metric.name,
			Timestamp: timestamp,
			Value:     metric.value,
			Tags:      baseTags,
		})
	}

	return nil
}

func (u *unixCollector) collectCPUUsage(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil {
		return fmt.Errorf("error getting CPU percentage metrics: %w", err)
	}

	if len(cpuPercent) > 0 {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      "cpu_usage_total",
			Timestamp: timestamp,
			Value:     cpuPercent[0],
			Tags:      baseTags,
		})
	}

	return nil
}

func (u *unixCollector) collectLoadAverage(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	loadAvg, err := load.Avg()
	if err != nil {
		return fmt.Errorf("error getting load average: %w", err)
	}

	metrics := []struct {
		name  string
		value float64
	}{
		{"cpu_load1", loadAvg.Load1},
		{"cpu_load5", loadAvg.Load5},
		{"cpu_load15", loadAvg.Load15},
	}

	for _, metric := range metrics {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      metric.name,
			Timestamp: timestamp,
			Value:     metric.value,
			Tags:      baseTags,
		})
	}

	return nil
}

func (u *unixCollector) collectPerCoreMetrics(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	perCpuPercent, err := cpu.Percent(time.Second, true)
	if err != nil {
		return fmt.Errorf("error getting per-CPU metrics: %w", err)
	}

	for i, cpuPercent := range perCpuPercent {
		coreTags := append([]tags.Tag{}, baseTags...)
		coreTags = append(coreTags, tags.Tag{
			Key:   "core",
			Value: fmt.Sprintf("%d", i),
		})

		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      "cpu_core_usage",
			Timestamp: timestamp,
			Value:     cpuPercent,
			Tags:      coreTags,
		})
	}

	return nil
}

// collectProcessesCount emits the count of running processes on the
// host. Sources `load.Misc()` which on Linux reads /proc/stat
// (procs_running + procs_blocked + ProcsTotal) — cheap, single file
// open. On Darwin and *BSD where /proc isn't available, gopsutil
// falls back to a sysctl call; same single-syscall cost.
//
// Mapped via cpu.yaml to OTel `system.processes.count` (gauge,
// unit `{process}`) per the OTel system semconv.
func (u *unixCollector) collectProcessesCount(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	misc, err := load.Misc()
	if err != nil {
		return fmt.Errorf("error getting process count: %w", err)
	}
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "cpu_processes_total",
		Timestamp: timestamp,
		Value:     float64(misc.ProcsTotal),
		Tags:      baseTags,
	})
	return nil
}

func (u *unixCollector) Close() error {
	return nil
}

// collectCPUCount emits the number of logical processors. Mapped to the
// OTel system.cpu.logical.count gauge.
func (u *unixCollector) collectCPUCount(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	n, err := cpu.Counts(true)
	if err != nil {
		return fmt.Errorf("error getting the processor count: %w", err)
	}
	if n <= 0 {
		return fmt.Errorf("the processor count came back as %d", n)
	}
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "cpu_count",
		Timestamp: timestamp,
		Value:     float64(n),
		Tags:      baseTags,
	})
	return nil
}

// kernelCounters is one reading of the kernel's cumulative counters.
type kernelCounters struct {
	Interrupts      float64
	ContextSwitches float64
	ProcsRunning    float64
}

// collectKernelCounters turns the cumulative counters into the rates the
// definitions declare, and emits the runnable process count as it is.
// The first reading has nothing to subtract from and emits only the
// gauge, the way the per-mode percentages warm up.
func (u *unixCollector) collectKernelCounters(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	curr, err := readKernelCounters()
	if err != nil {
		return err
	}
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "cpu_processes_running",
		Timestamp: timestamp,
		Value:     curr.ProcsRunning,
		Tags:      baseTags,
	})

	prev, prevAt := u.lastKernel, u.lastKernelAt
	u.lastKernel, u.lastKernelAt = curr, timestamp
	if prev == nil {
		return nil
	}
	elapsed := timestamp.Sub(prevAt).Seconds()
	if elapsed <= 0 {
		return nil
	}
	rate := func(now, before float64) (float64, bool) {
		// A counter that went backwards was reset, by a reboot or by the
		// container being replaced; skip the round rather than emit a
		// negative rate.
		if now < before {
			return 0, false
		}
		return (now - before) / elapsed, true
	}
	for _, m := range []struct {
		name        string
		now, before float64
	}{
		{"cpu_interrupts", curr.Interrupts, prev.Interrupts},
		{"cpu_context_switches", curr.ContextSwitches, prev.ContextSwitches},
	} {
		if v, ok := rate(m.now, m.before); ok {
			*dataPoints = append(*dataPoints, data_store.DataPoint{
				Name:      m.name,
				Timestamp: timestamp,
				Value:     v,
				Tags:      baseTags,
			})
		}
	}
	return nil
}
