package status

import (
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// processSample is what the operating system says about the agent's own
// process, as opposed to what the Go runtime knows about its heap.
type processSample struct {
	// RSSMB is the resident set (the working set on Windows).
	RSSMB float64
	// CPUPercent is the process's CPU time over the last interval, as a
	// share of the whole machine (all cores), like the Windows task
	// manager: 100 means every core busy with the agent.
	CPUPercent float64
	// Started is when the process was created; zero if unknown.
	Started time.Time
	OK      bool
}

// processSampler measures CPU as the CPU time consumed between two
// readings divided by the wall time between them. Readings closer than
// minInterval reuse the previous result, so several callers polling at
// once (the console header, the overview, the CLI) do not shrink the
// interval to nothing.
type processSampler struct {
	mu          sync.Mutex
	proc        *process.Process
	lastCPU     float64
	lastAt      time.Time
	last        processSample
	minInterval time.Duration
}

func newProcessSampler() *processSampler {
	s := &processSampler{minInterval: 2 * time.Second}
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return s
	}
	s.proc = p
	if t, err := p.Times(); err == nil {
		s.lastCPU = t.User + t.System
		s.lastAt = time.Now()
	}
	if ms, err := p.CreateTime(); err == nil {
		s.last.Started = time.UnixMilli(ms)
	}
	return s
}

func (s *processSampler) sample() processSample {
	if s == nil {
		return processSample{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return processSample{}
	}
	now := time.Now()
	if s.last.OK && now.Sub(s.lastAt) < s.minInterval {
		return s.last
	}

	out := processSample{Started: s.last.Started}
	if mem, err := s.proc.MemoryInfo(); err == nil {
		out.RSSMB = float64(mem.RSS) / 1024 / 1024
		out.OK = true
	}
	if t, err := s.proc.Times(); err == nil {
		cpu := t.User + t.System
		if wall := now.Sub(s.lastAt).Seconds(); !s.lastAt.IsZero() && wall > 0 && cpu >= s.lastCPU {
			out.CPUPercent = (cpu - s.lastCPU) / wall / float64(runtime.NumCPU()) * 100
		}
		s.lastCPU, s.lastAt = cpu, now
	} else {
		out.OK = false
	}
	s.last = out
	return out
}
