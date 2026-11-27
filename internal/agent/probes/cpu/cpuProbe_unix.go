//go:build !windows

// internal/agent/probes/host/cpuProbe_unix.go
package cpu

import (
	"errors"
	"fmt"
	"math"
	"os"
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
	// lastPerCore holds the previous per-core cumulative CPU time
	// snapshots, used to derive per-core utilization percentages from
	// deltas instead of gopsutil's blocking cpu.Percent call.
	lastPerCore []cpu.TimesStat
	// timesUnavailable is set when cpu.Times() fails or returns nothing
	// (e.g. macOS without cgo), so the usage collectors fall back to
	// the blocking cpu.Percent path.
	timesUnavailable bool
	// lastKernel holds the previous reading of the kernel's cumulative
	// counters, which are rates once divided by the elapsed time. The
	// definitions declare them in 1/s, the way the Windows collector
	// already reports them.
	lastKernel   *kernelCounters
	lastKernelAt time.Time

	times         func(perCPU bool) ([]cpu.TimesStat, error)
	kernel        func() (*kernelCounters, error)
	lastPerCoreAt time.Time
	statePath     string
	stateMaxAge   time.Duration
	warm          *cpuWarmState
}

// timesDelta returns the user and nice deltas (with guest time netted
// out when the result stays non-negative, since the kernel accounts
// guest inside user and guest_nice inside nice) plus the total delta
// over the ten modes, so every CPU percentage shares one denominator.
func timesDelta(prev, curr cpu.TimesStat) (user, nice, total float64) {
	guestDelta := curr.Guest - prev.Guest
	guestNiceDelta := curr.GuestNice - prev.GuestNice
	user = curr.User - prev.User
	nice = curr.Nice - prev.Nice
	if user-guestDelta >= 0 {
		user -= guestDelta
	}
	if nice-guestNiceDelta >= 0 {
		nice -= guestNiceDelta
	}
	total = user +
		(curr.System - prev.System) +
		(curr.Idle - prev.Idle) +
		nice +
		(curr.Iowait - prev.Iowait) +
		(curr.Irq - prev.Irq) +
		(curr.Softirq - prev.Softirq) +
		(curr.Steal - prev.Steal) +
		guestDelta +
		guestNiceDelta
	return user, nice, total
}

// busyPercent derives the overall busy percentage over an interval from
// two cumulative snapshots, using the same total (and thus the same
// guest subtraction) as the per-mode percentages. Returns false when
// total <= 0, so callers can skip the round instead of emitting
// divide-by-zero values.
func busyPercent(prev, curr cpu.TimesStat) (float64, bool) {
	_, _, total := timesDelta(prev, curr)
	if total <= 0 {
		return 0, false
	}
	busy := total - (curr.Idle - prev.Idle) - (curr.Iowait - prev.Iowait)
	v := (busy / total) * 100.0
	if v < 0 {
		return 0, true
	}
	if v > 100 {
		return 100, true
	}
	return v, true
}

// perCorePercents derives one busy percentage per core from previous and
// current cumulative snapshots. Returns false when the lengths differ or
// prev is empty (first call or core-count change), meaning the caller
// should only memorize the current snapshot.
func perCorePercents(prev, curr []cpu.TimesStat) ([]float64, bool) {
	if len(prev) == 0 || len(prev) != len(curr) {
		return nil, false
	}
	percents := make([]float64, len(curr))
	for i := range curr {
		v, ok := busyPercent(prev[i], curr[i])
		if !ok {
			// A core with no ticks in the interval says nothing; the
			// others still do.
			v = math.NaN()
		}
		percents[i] = v
	}
	return percents, true
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
	times, err := u.readTimes(false)
	if err != nil {
		u.timesUnavailable = true
		return fmt.Errorf("error getting CPU times: %w", err)
	}

	if len(times) == 0 {
		// Platform without a working cpu.Times (e.g. macOS without cgo):
		// signal the fallback to the blocking cpu.Percent path.
		u.timesUnavailable = true
		return nil
	}

	u.timesUnavailable = false

	curr := times[0]

	if u.lastTimes == nil && u.warm != nil && u.warm.Times != nil {
		if u.warmUsable(u.warm.TimesAt, timestamp) {
			u.lastTimes = u.warm.Times
			u.lastTimestamp = u.warm.TimesAt
		}
		u.warm.Times = nil
	}

	// On the first collection we have nothing to diff against — store the
	// snapshot and return without emitting per-mode percentages. The first
	// real datapoints arrive on the second tick of the probe interval.
	// cpu_usage_total and per-core values come from these same deltas and
	// follow the same warm-up.
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
	// only taken when it stays positive (see timesDelta).
	userDelta, niceDelta, totalDelta := timesDelta(prev, curr)

	guestDelta := curr.Guest - prev.Guest
	guestNiceDelta := curr.GuestNice - prev.GuestNice

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

	if usage, ok := busyPercent(prev, curr); ok {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      "cpu_usage_total",
			Timestamp: timestamp,
			Value:     usage,
			Tags:      baseTags,
		})
	}

	return nil
}

func (u *unixCollector) collectCPUUsage(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	// collectCPUTimes derives cpu_usage_total from the same deltas as the
	// per-mode metrics; only fall back to the blocking cpu.Percent call on
	// platforms where cpu.Times is unavailable (e.g. macOS without cgo).
	if !u.timesUnavailable {
		return nil
	}

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
	times, err := u.readTimes(true)
	if err == nil && len(times) > 0 {
		if u.lastPerCore == nil && u.warm != nil && len(u.warm.PerCore) > 0 {
			if u.warmUsable(u.warm.PerCoreAt, timestamp) {
				u.lastPerCore = u.warm.PerCore
			}
			u.warm.PerCore = nil
		}
		if percents, ok := perCorePercents(u.lastPerCore, times); ok {
			for i, cpuPercent := range percents {
				if math.IsNaN(cpuPercent) {
					continue
				}
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
		}
		// Memorize even on the first call or a core-count change, so the
		// next tick has something to diff against.
		u.lastPerCore = times
		u.lastPerCoreAt = timestamp
		return nil
	}

	// The next delta must not span the interval this sample covered.
	u.lastPerCore = nil
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
	if u.statePath == "" || u.lastTimes == nil {
		return nil
	}
	boot, err := hostpoll.BootIdentity()
	if err != nil {
		u.logger.Debug().Err(err).Msg("Warm state not saved: no boot identity")
		return nil
	}
	st := cpuWarmState{
		BootID:    boot,
		TimesAt:   u.lastTimestamp,
		Times:     u.lastTimes,
		PerCoreAt: u.lastPerCoreAt,
		PerCore:   u.lastPerCore,
		KernelAt:  u.lastKernelAt,
		Kernel:    u.lastKernel,
	}
	if err := hostpoll.SaveWarm(u.statePath, &st); err != nil {
		u.logger.Warn().Err(err).Str("path", u.statePath).Msg("Warm state save failed")
	}
	return nil
}

func (u *unixCollector) SetState(path string, maxAge time.Duration) {
	u.statePath = path
	u.stateMaxAge = maxAge
	var st cpuWarmState
	if err := hostpoll.TakeWarm(path, &st); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			u.logger.Debug().Err(err).Str("path", path).Msg("Discarding unusable warm state")
		}
		return
	}
	u.warm = &st
}

func (u *unixCollector) warmUsable(readAt, now time.Time) bool {
	return u.warm != nil && hostpoll.WarmUsable(u.warm.BootID, readAt, now, u.stateMaxAge)
}

func (u *unixCollector) readTimes(perCPU bool) ([]cpu.TimesStat, error) {
	if u.times == nil {
		return cpu.Times(perCPU)
	}
	return u.times(perCPU)
}

func (u *unixCollector) readKernel() (*kernelCounters, error) {
	if u.kernel == nil {
		return readKernelCounters()
	}
	return u.kernel()
}

type cpuWarmState struct {
	BootID    string          `json:"boot_id"`
	TimesAt   time.Time       `json:"times_at"`
	Times     *cpu.TimesStat  `json:"times,omitempty"`
	PerCoreAt time.Time       `json:"per_core_at"`
	PerCore   []cpu.TimesStat `json:"per_core,omitempty"`
	KernelAt  time.Time       `json:"kernel_at"`
	Kernel    *kernelCounters `json:"kernel,omitempty"`
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
	curr, err := u.readKernel()
	if err != nil {
		return err
	}
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "cpu_processes_running",
		Timestamp: timestamp,
		Value:     curr.ProcsRunning,
		Tags:      baseTags,
	})

	if u.lastKernel == nil && u.warm != nil && u.warm.Kernel != nil {
		if u.warmUsable(u.warm.KernelAt, timestamp) {
			u.lastKernel = u.warm.Kernel
			u.lastKernelAt = u.warm.KernelAt
		}
		u.warm.Kernel = nil
	}
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
