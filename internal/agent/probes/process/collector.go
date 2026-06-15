//go:build linux || windows || darwin

package process

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"time"

	gops "github.com/shirou/gopsutil/v3/process"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// processSnapshot holds one cycle of data for a single process.
type processSnapshot struct {
	pid        int32
	name       string
	owner      string
	cpuPct     float64 // 0-100, to be divided to 0-1 for OTel
	rss        uint64  // bytes
	vms        uint64  // bytes
	threads    int32
	fds        int32 // -1 on Windows / unsupported
	createTime int64 // Unix ms
}

// collect enumerates processes, applies filters, and builds datapoints.
func collect(ts time.Time, cfg config, log *logger.ModuleLogger) ([]data_store.DataPoint, error) {
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("host tags: %w", err)
	}

	hostname, _ := os.Hostname()

	procs, err := gops.Processes()
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}

	snaps := make([]processSnapshot, 0, len(procs))
	for _, p := range procs {
		snap, skip := snapshotProcess(p, cfg, log)
		if skip {
			continue
		}
		snaps = append(snaps, snap)
	}

	// top_n filter: keep the N processes with highest CPU usage.
	if cfg.topN > 0 && len(snaps) > cfg.topN {
		sort.Slice(snaps, func(i, j int) bool {
			return snaps[i].cpuPct > snaps[j].cpuPct
		})
		snaps = snaps[:cfg.topN]
	}

	points := make([]data_store.DataPoint, 0, len(snaps)*7)

	for _, snap := range snaps {
		processTags := buildProcessTags(baseTags, snap, hostname)
		points = appendProcessPoints(points, ts, snap, processTags)
	}

	// Aggregated process.count per name.
	if cfg.aggregate {
		counts := map[string]int{}
		for _, snap := range snaps {
			counts[snap.name]++
		}
		for name, cnt := range counts {
			aggTags := append([]tags.Tag{}, baseTags...)
			aggTags = append(aggTags,
				tags.Tag{Key: "process.name", Value: name},
			)
			points = append(points, data_store.DataPoint{
				Name:      "process.count",
				Timestamp: ts,
				Value:     float32(cnt),
				Tags:      aggTags,
			})
		}
	}

	return points, nil
}

// snapshotProcess reads one process and returns (snap, skip).
// skip is true when the process should be excluded by filter or is gone.
func snapshotProcess(p *gops.Process, cfg config, log *logger.ModuleLogger) (processSnapshot, bool) {
	name, err := p.Name()
	if err != nil {
		return processSnapshot{}, true
	}

	if cfg.byName != nil && !cfg.byName.MatchString(name) {
		return processSnapshot{}, true
	}

	owner := ""
	if u, err := p.Username(); err == nil {
		owner = u
	}

	if cfg.byUser != "" && owner != cfg.byUser {
		return processSnapshot{}, true
	}

	cpuPct, err := p.CPUPercent()
	if err != nil {
		log.Debug().Int32("pid", p.Pid).Err(err).Msg("CPUPercent unavailable")
		cpuPct = 0
	}

	var rss, vms uint64
	if mem, err := p.MemoryInfo(); err == nil && mem != nil {
		rss = mem.RSS
		vms = mem.VMS
	}

	threads, _ := p.NumThreads()

	fds := int32(-1)
	if runtime.GOOS != "windows" {
		if n, err := p.NumFDs(); err == nil {
			fds = n
		}
	}

	createTime, _ := p.CreateTime()

	return processSnapshot{
		pid:        p.Pid,
		name:       name,
		owner:      owner,
		cpuPct:     cpuPct,
		rss:        rss,
		vms:        vms,
		threads:    threads,
		fds:        fds,
		createTime: createTime,
	}, false
}

// buildProcessTags constructs the per-process tag set.
func buildProcessTags(baseTags []tags.Tag, snap processSnapshot, hostname string) []tags.Tag {
	pt := append([]tags.Tag{}, baseTags...)
	pt = append(pt,
		tags.Tag{Key: "process.pid", Value: strconv.Itoa(int(snap.pid))},
		tags.Tag{Key: "process.name", Value: snap.name},
	)
	if snap.owner != "" {
		pt = append(pt, tags.Tag{Key: "process.owner", Value: snap.owner})
	}

	// OTel service.instance.id — identifies the running process uniquely
	// across the host without needing Toise or a custom entity type.
	serviceInstanceID := "process://" + hostname + "/" + snap.name + "/" + strconv.Itoa(int(snap.pid))
	pt = append(pt, tags.Tag{Key: "service.instance.id", Value: serviceInstanceID})

	return pt
}

// appendProcessPoints emits the per-process metric datapoints.
func appendProcessPoints(
	points []data_store.DataPoint,
	ts time.Time,
	snap processSnapshot,
	pt []tags.Tag,
) []data_store.DataPoint {
	// process.cpu.utilization — ratio 0-1 (OTel semconv)
	points = append(points, data_store.DataPoint{
		Name:      "process.cpu.utilization",
		Timestamp: ts,
		Value:     float32(snap.cpuPct / 100.0),
		Tags:      pt,
	})

	// process.memory.usage — RSS bytes
	points = append(points, data_store.DataPoint{
		Name:      "process.memory.usage",
		Timestamp: ts,
		Value:     float32(snap.rss),
		Tags:      pt,
	})

	// process.memory.virtual_memory_usage — VMS bytes
	points = append(points, data_store.DataPoint{
		Name:      "process.memory.virtual_memory_usage",
		Timestamp: ts,
		Value:     float32(snap.vms),
		Tags:      pt,
	})

	// process.threads
	points = append(points, data_store.DataPoint{
		Name:      "process.threads",
		Timestamp: ts,
		Value:     float32(snap.threads),
		Tags:      pt,
	})

	// process.open_file_descriptors — Linux only; skip on Windows (fds == -1)
	if snap.fds >= 0 {
		points = append(points, data_store.DataPoint{
			Name:      "process.open_file_descriptors",
			Timestamp: ts,
			Value:     float32(snap.fds),
			Tags:      pt,
		})
	}

	// process.uptime — seconds since creation
	if snap.createTime > 0 {
		uptimeS := ts.UnixMilli() - snap.createTime
		if uptimeS < 0 {
			uptimeS = 0
		}
		points = append(points, data_store.DataPoint{
			Name:      "process.uptime",
			Timestamp: ts,
			Value:     float32(uptimeS) / 1000.0,
			Tags:      pt,
		})
	}

	return points
}
