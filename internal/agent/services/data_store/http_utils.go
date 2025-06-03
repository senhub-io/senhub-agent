// senhub-agent/internal/agent/services/data_store/http_utils.go
package data_store

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// CPUMeasurement holds CPU timing state for usage calculations
type CPUMeasurement struct {
	LastCPUTime        time.Duration
	LastMeasurementTime time.Time
	StartTime          time.Time
}

// NewCPUMeasurement creates a new CPU measurement tracker
func NewCPUMeasurement() *CPUMeasurement {
	return &CPUMeasurement{
		StartTime: time.Now(),
	}
}

// GetCPUUsage calculates the CPU usage percentage for the current process
func (c *CPUMeasurement) GetCPUUsage() float64 {
	// Get current process CPU usage
	pid := os.Getpid()
	
	// Read CPU times from platform-specific sources
	var currentCPUTime time.Duration
	var err error
	
	switch runtime.GOOS {
	case "linux":
		currentCPUTime, err = getCPUTimeLinux(pid)
	case "darwin":
		currentCPUTime, err = getCPUTimeDarwin(pid, c.StartTime)
	default:
		// Fallback: return 0 for unsupported platforms
		return 0.0
	}
	
	if err != nil {
		return 0.0
	}
	
	now := time.Now()
	
	// If this is the first measurement, store the values and return 0
	if c.LastMeasurementTime.IsZero() {
		c.LastCPUTime = currentCPUTime
		c.LastMeasurementTime = now
		return 0.0
	}
	
	// Calculate CPU usage percentage
	cpuTimeDelta := currentCPUTime - c.LastCPUTime
	wallTimeDelta := now.Sub(c.LastMeasurementTime)
	
	// Update stored values for next calculation
	c.LastCPUTime = currentCPUTime
	c.LastMeasurementTime = now
	
	if wallTimeDelta == 0 {
		return 0.0
	}
	
	// CPU percentage = (CPU time delta / wall time delta) * 100
	cpuPercent := float64(cpuTimeDelta) / float64(wallTimeDelta) * 100.0
	
	// Cap at 100% (can exceed on multi-core systems)
	if cpuPercent > 100.0 {
		cpuPercent = 100.0
	}
	
	return cpuPercent
}

// getCPUTimeLinux reads CPU time from /proc/pid/stat on Linux
func getCPUTimeLinux(pid int) (time.Duration, error) {
	statFile := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statFile)
	if err != nil {
		return 0, err
	}
	
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0, fmt.Errorf("insufficient fields in /proc/stat")
	}
	
	// Fields 13 and 14 contain user and system CPU time in clock ticks
	utime, err := strconv.ParseInt(fields[13], 10, 64)
	if err != nil {
		return 0, err
	}
	
	stime, err := strconv.ParseInt(fields[14], 10, 64)
	if err != nil {
		return 0, err
	}
	
	// Convert clock ticks to time duration
	// Get clock ticks per second (usually 100 on most Linux systems)
	clockTicks := int64(100)
	
	totalTicks := utime + stime
	totalNanos := (totalTicks * int64(time.Second)) / clockTicks
	
	return time.Duration(totalNanos), nil
}

// getCPUTimeDarwin reads CPU time on macOS using runtime stats
func getCPUTimeDarwin(pid int, startTime time.Time) (time.Duration, error) {
	// On macOS, we'll use runtime stats as a fallback since syscall.Getrusage is not available
	if pid != os.Getpid() {
		return 0, fmt.Errorf("can only get CPU time for current process on macOS")
	}
	
	// Get memory stats which include runtime information
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	
	// Estimate CPU time based on GC pause times and other runtime metrics
	// This is a simplified approach since direct CPU time access requires platform-specific code
	gcPauseTotal := time.Duration(0)
	for i := 0; i < len(stats.PauseNs); i++ {
		gcPauseTotal += time.Duration(stats.PauseNs[i])
	}
	
	// Return estimated CPU time based on GC activity and uptime
	// This is an approximation since we can't access rusage on this platform
	uptime := time.Since(startTime)
	estimatedCPUTime := uptime/10 + gcPauseTotal // rough estimate
	
	return estimatedCPUTime, nil
}