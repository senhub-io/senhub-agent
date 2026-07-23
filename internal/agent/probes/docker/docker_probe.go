// Package docker implements the Pro-tier docker probe: per-container
// resource metrics (CPU, memory, network I/O, block I/O, restart count,
// up/down status) collected from the Docker Engine API via its Unix socket.
// No external SDK dependency — the API is consumed with stdlib net/http.
// Containers are discovered dynamically each cycle (no pre-configuration
// required). Entity source emits one container entity per running container
// into the entity rail for Toise topology (#392). Docker Swarm service/task
// topology is out of scope for this PR (#397).
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier — used in JWT claims,
// transformer YAML probe_name, and DiscriminantTagsRegistry.
const ProbeType = "docker"

const (
	defaultSocketPath     = "/var/run/docker.sock"
	defaultInterval       = 60 * time.Second
	defaultTimeout        = 10 * time.Second
	apiVersion            = "v1.43"
	maxParallelContainers = 16
)

// probeConfig holds the parsed probe configuration.
type probeConfig struct {
	SocketPath string
	Interval   time.Duration
	Timeout    time.Duration
	Include    []string // glob patterns for container names (empty = all)
	Exclude    []string // glob patterns to exclude
}

// dockerProbe collects Docker container metrics from the Engine API.
type dockerProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *dockerEntitySource
	// newClient allows tests to inject a replacement transport.
	newClient func() *http.Client
}

// containerListItem is the shape of one element in GET /containers/json.
type containerListItem struct {
	ID           string   `json:"Id"`
	Names        []string `json:"Names"`
	Image        string   `json:"Image"`
	State        string   `json:"State"`
	RestartCount int      `json:"RestartCount"`
}

// blkioEntry is a single entry in Docker's blkio recursive arrays.
type blkioEntry struct {
	Op    string `json:"op"`
	Value uint64 `json:"value"`
}

// containerStats is the shape of GET /containers/{id}/stats?stream=false.
type containerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage        uint64   `json:"total_usage"`
			UsageInKernelmode uint64   `json:"usage_in_kernelmode"`
			UsageInUsermode   uint64   `json:"usage_in_usermode"`
			PercpuUsage       []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int    `json:"online_cpus"`
		ThrottlingData struct {
			ThrottledPeriods  uint64 `json:"throttled_periods"`
			ThrottlingPeriods uint64 `json:"throttling_periods"`
			ThrottledTime     uint64 `json:"throttled_time"`
		} `json:"throttling_data"`
	} `json:"cpu_stats"`
	// PreCPUStats is the previous snapshot included in the same response payload.
	// It is used to derive cpu.percent without a second API call.
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"` // cgroupsv1: rss/cache/swap; cgroupsv2: anon/file/inactive_file
	} `json:"memory_stats"`
	Networks map[string]struct {
		TxBytes   uint64 `json:"tx_bytes"`
		RxBytes   uint64 `json:"rx_bytes"`
		TxPackets uint64 `json:"tx_packets"`
		RxPackets uint64 `json:"rx_packets"`
		TxErrors  uint64 `json:"tx_errors"`
		RxErrors  uint64 `json:"rx_errors"`
		TxDropped uint64 `json:"tx_dropped"`
		RxDropped uint64 `json:"rx_dropped"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []blkioEntry `json:"io_service_bytes_recursive"`
		// io_service_time_recursive and io_sectors_recursive are present on
		// cgroupsv1 kernels; absent on cgroupsv2. Parse defensively.
		IOServiceTimeRecursive []blkioEntry `json:"io_service_time_recursive"`
		IOSectorsRecursive     []blkioEntry `json:"io_sectors_recursive"`
	} `json:"blkio_stats"`
	PidsStats struct {
		Current uint64 `json:"current"`
		Limit   uint64 `json:"limit"`
	} `json:"pids_stats"`
}

// statsResult pairs a container with its collected stats or an error.
type statsResult struct {
	container containerListItem
	stats     *containerStats
	err       error
}

// NewDockerProbe constructs the Docker probe. Config errors surface here.
func NewDockerProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.docker")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	p := &dockerProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		entitySrc:    &dockerEntitySource{},
	}
	p.SetProbeType(ProbeType)
	// Containers are distinct entities, not host-local: declare the real
	// source so the poller registers it on Start.
	p.SetEntitySource(p.entitySrc)
	p.newClient = p.buildClient
	p.client = p.buildClient()
	return p, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		SocketPath: defaultSocketPath,
		Interval:   defaultInterval,
		Timeout:    defaultTimeout,
	}

	if v, ok := config["socket_path"].(string); ok && v != "" {
		cfg.SocketPath = v
	}
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := types.IntParam(config, "timeout"); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}

	if raw, ok := config["include"]; ok {
		switch v := raw.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					cfg.Include = append(cfg.Include, s)
				}
			}
		case []string:
			cfg.Include = v
		}
	}
	if raw, ok := config["exclude"]; ok {
		switch v := raw.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					cfg.Exclude = append(cfg.Exclude, s)
				}
			}
		case []string:
			cfg.Exclude = v
		}
	}
	return cfg, nil
}

// buildClient constructs an http.Client that dials the Unix socket.
func (p *dockerProbe) buildClient() *http.Client {
	socketPath := p.cfg.SocketPath
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   p.cfg.Timeout,
	}
}

func (p *dockerProbe) GetInterval() time.Duration { return p.cfg.Interval }
func (p *dockerProbe) ShouldStart() bool          { return true }

func (p *dockerProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Str("socket", p.cfg.SocketPath).
		Msg("Starting docker probe")
	return nil
}

func (p *dockerProbe) OnShutdown(ctx context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches the container list and per-container stats from the Docker
// Engine API. A container that is stopped or disappears between list and stats
// emits senhub.docker.up=0 without resource metrics — never a collection error.
func (p *dockerProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	containers, socketOK, err := p.listContainers()
	if err != nil {
		// Socket unreachable — not a fatal collection error: emit nothing and
		// let the scheduler retry. The absence of series tells PRTG the sensor
		// is down; a persistent failure will show in the agent's self-metrics.
		return nil, fmt.Errorf("docker: listing containers: %w", err)
	}
	if !socketOK {
		return nil, nil
	}

	containers = p.applyFilter(containers)

	// Fetch stats concurrently (bounded).
	results := make([]statsResult, len(containers))
	sem := make(chan struct{}, maxParallelContainers)
	var wg sync.WaitGroup
	for i, c := range containers {
		wg.Add(1)
		go func(i int, c containerListItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			stats, err := p.fetchStats(c.ID)
			results[i] = statsResult{container: c, stats: stats, err: err}
		}(i, c)
	}
	wg.Wait()

	// Update entity cache from the live container list (no extra API call needed).
	p.entitySrc.update(containers)

	var points []data_store.DataPoint
	for _, res := range results {
		points = append(points, p.buildDatapoints(res, now)...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// listContainers calls GET /containers/json?all=true. Returns ok=false when
// the socket dial itself fails (engine not running); a non-2xx status or a
// JSON error is a real error.
func (p *dockerProbe) listContainers() ([]containerListItem, bool, error) {
	url := fmt.Sprintf("http://localhost/%s/containers/json?all=true", apiVersion)
	resp, err := p.client.Get(url)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, true, fmt.Errorf("docker containers/json returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("reading containers/json body: %w", err)
	}
	var list []containerListItem
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, true, fmt.Errorf("decoding containers/json: %w", err)
	}
	return list, true, nil
}

// fetchStats calls GET /containers/{id}/stats?stream=false. Returns nil when
// the container has stopped (HTTP 409) or disappeared (HTTP 404) — both cases
// are normal race conditions in a dynamic environment.
func (p *dockerProbe) fetchStats(id string) (*containerStats, error) {
	url := fmt.Sprintf("http://localhost/%s/containers/%s/stats?stream=false", apiVersion, id)
	resp, err := p.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusNotFound {
		// Container stopped or disappeared between list and stats — not an error.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker stats returned %d for container %s", resp.StatusCode, id)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading stats body for container %s: %w", id, err)
	}
	var s containerStats
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("decoding stats for container %s: %w", id, err)
	}
	return &s, nil
}

// applyFilter retains only containers whose primary name matches the include
// list and does not match the exclude list. Both lists use stdlib path.Match
// (glob). When include is empty all containers pass the include step.
func (p *dockerProbe) applyFilter(containers []containerListItem) []containerListItem {
	if len(p.cfg.Include) == 0 && len(p.cfg.Exclude) == 0 {
		return containers
	}
	out := containers[:0]
	for _, c := range containers {
		name := primaryName(c)
		if !p.matchesInclude(name) {
			continue
		}
		if p.matchesExclude(name) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (p *dockerProbe) matchesInclude(name string) bool {
	if len(p.cfg.Include) == 0 {
		return true
	}
	for _, pattern := range p.cfg.Include {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

func (p *dockerProbe) matchesExclude(name string) bool {
	for _, pattern := range p.cfg.Exclude {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

// buildDatapoints converts one statsResult into the metric set for that container.
func (p *dockerProbe) buildDatapoints(res statsResult, ts time.Time) []data_store.DataPoint {
	name := primaryName(res.container)
	baseTags := []tags.Tag{
		{Key: "container_id", Value: shortID(res.container.ID)},
		{Key: "container_name", Value: name},
		{Key: "image", Value: res.container.Image},
	}

	up := float64(0)
	if res.container.State == "running" && res.stats != nil {
		up = 1
	}
	if res.err != nil {
		p.moduleLogger.Warn().
			Err(res.err).
			Str("container", name).
			Msg("docker stats fetch failed")
	}

	statusTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "status"})
	points := []data_store.DataPoint{
		{Name: "senhub.docker.up", Value: up, Timestamp: ts, Tags: statusTags},
		{Name: "container.restarts", Value: float64(res.container.RestartCount), Timestamp: ts, Tags: statusTags},
	}

	if res.stats == nil {
		// Stopped or disappeared container: emit status metrics only.
		return points
	}

	s := res.stats

	// CPU metrics.
	cpuTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "cpu"})
	cpuOnline := s.CPUStats.OnlineCPUs
	if cpuOnline == 0 {
		cpuOnline = len(s.CPUStats.CPUUsage.PercpuUsage)
	}
	points = append(points,
		data_store.DataPoint{Name: "container.cpu.usage.total", Value: float64(s.CPUStats.CPUUsage.TotalUsage), Timestamp: ts, Tags: cpuTags},
		data_store.DataPoint{Name: "container.cpu.usage.kernelmode", Value: float64(s.CPUStats.CPUUsage.UsageInKernelmode), Timestamp: ts, Tags: cpuTags},
		data_store.DataPoint{Name: "container.cpu.usage.usermode", Value: float64(s.CPUStats.CPUUsage.UsageInUsermode), Timestamp: ts, Tags: cpuTags},
		data_store.DataPoint{Name: "senhub.docker.cpu.system", Value: float64(s.CPUStats.SystemCPUUsage), Timestamp: ts, Tags: cpuTags},
		data_store.DataPoint{Name: "senhub.docker.cpu.online", Value: float64(cpuOnline), Timestamp: ts, Tags: cpuTags},
	)

	// Derived cpu.percent — same formula used by `docker stats`.
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
	if systemDelta > 0 && cpuDelta >= 0 {
		cpuPercent := (cpuDelta / systemDelta) * float64(cpuOnline) * 100.0
		points = append(points,
			data_store.DataPoint{Name: "senhub.docker.cpu.percent", Value: float64(cpuPercent), Timestamp: ts, Tags: cpuTags},
		)
	}

	// Per-core CPU usage — one datapoint per core with tag core=N.
	for i, coreUsage := range s.CPUStats.CPUUsage.PercpuUsage {
		coreTags := append(append([]tags.Tag{}, baseTags...),
			tags.Tag{Key: "metric_type", Value: "cpu"},
			tags.Tag{Key: "core", Value: strconv.Itoa(i)},
		)
		points = append(points,
			data_store.DataPoint{Name: "container.cpu.usage.percpu", Value: float64(coreUsage), Timestamp: ts, Tags: coreTags},
		)
	}

	// CPU throttling metrics (cgroupsv1 + cgroupsv2, always present in the response).
	points = append(points,
		data_store.DataPoint{Name: "container.cpu.throttling_data.throttled_periods", Value: float64(s.CPUStats.ThrottlingData.ThrottledPeriods), Timestamp: ts, Tags: cpuTags},
		data_store.DataPoint{Name: "container.cpu.throttling_data.periods", Value: float64(s.CPUStats.ThrottlingData.ThrottlingPeriods), Timestamp: ts, Tags: cpuTags},
		data_store.DataPoint{Name: "container.cpu.throttling_data.throttled_time", Value: float64(s.CPUStats.ThrottlingData.ThrottledTime), Timestamp: ts, Tags: cpuTags},
	)

	// Memory metrics — cgroups v1/v2 detection via presence of "rss" key.
	memTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "memory"})
	points = append(points,
		data_store.DataPoint{Name: "container.memory.usage", Value: float64(s.MemoryStats.Usage), Timestamp: ts, Tags: memTags},
		data_store.DataPoint{Name: "senhub.docker.memory.limit", Value: float64(s.MemoryStats.Limit), Timestamp: ts, Tags: memTags},
	)

	_, isCgroupV1 := s.MemoryStats.Stats["rss"]
	if isCgroupV1 {
		// cgroups v1 keys.
		rss := s.MemoryStats.Stats["rss"]
		cache := s.MemoryStats.Stats["cache"]
		swap := s.MemoryStats.Stats["swap"]
		var workingSet uint64
		if s.MemoryStats.Usage > cache {
			workingSet = s.MemoryStats.Usage - cache
		}
		points = append(points,
			data_store.DataPoint{Name: "container.memory.rss", Value: float64(rss), Timestamp: ts, Tags: memTags},
			data_store.DataPoint{Name: "container.memory.cache", Value: float64(cache), Timestamp: ts, Tags: memTags},
			data_store.DataPoint{Name: "container.memory.swap", Value: float64(swap), Timestamp: ts, Tags: memTags},
			data_store.DataPoint{Name: "senhub.docker.memory.working_set", Value: float64(workingSet), Timestamp: ts, Tags: memTags},
		)
	} else {
		// cgroups v2 keys.
		anon := s.MemoryStats.Stats["anon"]
		file := s.MemoryStats.Stats["file"]
		inactiveFile := s.MemoryStats.Stats["inactive_file"]
		swap := s.MemoryStats.Stats["swap"]
		var workingSet uint64
		if s.MemoryStats.Usage > inactiveFile {
			workingSet = s.MemoryStats.Usage - inactiveFile
		}
		points = append(points,
			data_store.DataPoint{Name: "container.memory.rss", Value: float64(anon), Timestamp: ts, Tags: memTags},
			data_store.DataPoint{Name: "container.memory.cache", Value: float64(file), Timestamp: ts, Tags: memTags},
			data_store.DataPoint{Name: "container.memory.swap", Value: float64(swap), Timestamp: ts, Tags: memTags},
			data_store.DataPoint{Name: "senhub.docker.memory.working_set", Value: float64(workingSet), Timestamp: ts, Tags: memTags},
		)
	}

	// Deep cgroup memory stats — emitted only when the kernel exposes the field
	// (presence varies by kernel version and cgroup v1/v2). Mirrors the OTel
	// docker stats receiver contrib coverage.
	//
	// container.memory.anon: for v1 kernels the "anon" key may be absent; fall
	// back to "rss" which is the same concept on v1.
	if anon, ok := s.MemoryStats.Stats["anon"]; ok {
		points = append(points, data_store.DataPoint{Name: "container.memory.anon", Value: float64(anon), Timestamp: ts, Tags: memTags})
	} else if rss, ok := s.MemoryStats.Stats["rss"]; ok {
		points = append(points, data_store.DataPoint{Name: "container.memory.anon", Value: float64(rss), Timestamp: ts, Tags: memTags})
	}
	for _, kv := range []struct {
		metricName string
		statsKey   string
	}{
		{"container.memory.mapped_file", "mapped_file"},
		{"container.memory.pgfault", "pgfault"},
		{"container.memory.pgmajfault", "pgmajfault"},
		{"container.memory.unevictable", "unevictable"},
		{"container.memory.writeback", "writeback"},
		{"container.memory.hierarchical_memory_limit", "hierarchical_memory_limit"},
		{"container.memory.active_anon", "active_anon"},
		{"container.memory.inactive_anon", "inactive_anon"},
		{"container.memory.active_file", "active_file"},
		{"container.memory.inactive_file", "inactive_file"},
	} {
		if val, ok := s.MemoryStats.Stats[kv.statsKey]; ok {
			points = append(points, data_store.DataPoint{Name: kv.metricName, Value: float64(val), Timestamp: ts, Tags: memTags})
		}
	}

	// PIDs metrics.
	pidsTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "pids"})
	points = append(points,
		data_store.DataPoint{Name: "container.pids.count", Value: float64(s.PidsStats.Current), Timestamp: ts, Tags: pidsTags},
		data_store.DataPoint{Name: "senhub.docker.pids.limit", Value: float64(s.PidsStats.Limit), Timestamp: ts, Tags: pidsTags},
	)

	// Network metrics — absent for --network=host containers (nil map).
	if len(s.Networks) > 0 {
		var txBytes, rxBytes, txPkts, rxPkts, txErrors, rxErrors, txDropped, rxDropped uint64
		for _, iface := range s.Networks {
			txBytes += iface.TxBytes
			rxBytes += iface.RxBytes
			txPkts += iface.TxPackets
			rxPkts += iface.RxPackets
			txErrors += iface.TxErrors
			rxErrors += iface.RxErrors
			txDropped += iface.TxDropped
			rxDropped += iface.RxDropped
		}
		netTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "network"})
		points = append(points,
			data_store.DataPoint{Name: "container.network.io.usage.tx_bytes", Value: float64(txBytes), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "container.network.io.usage.rx_bytes", Value: float64(rxBytes), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "senhub.docker.network.tx_packets", Value: float64(txPkts), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "senhub.docker.network.rx_packets", Value: float64(rxPkts), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "container.network.io.usage.tx_errors", Value: float64(txErrors), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "container.network.io.usage.rx_errors", Value: float64(rxErrors), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "senhub.docker.network.tx_dropped", Value: float64(txDropped), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "senhub.docker.network.rx_dropped", Value: float64(rxDropped), Timestamp: ts, Tags: netTags},
		)
	}

	// Block I/O metrics — op="Total" is the canonical sum on cgroupsv1; fall
	// back to summing Read+Write when Total is absent (cgroupsv2 path).
	blkTotal, blkRead, blkWrite := blkioSplit(s.BlkioStats.IOServiceBytesRecursive)
	blkioTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "blkio"})
	points = append(points,
		data_store.DataPoint{Name: "container.blockio.usage.total", Value: float64(blkTotal), Timestamp: ts, Tags: blkioTags},
		data_store.DataPoint{Name: "container.blockio.io_service_bytes_recursive.read", Value: float64(blkRead), Timestamp: ts, Tags: blkioTags},
		data_store.DataPoint{Name: "container.blockio.io_service_bytes_recursive.write", Value: float64(blkWrite), Timestamp: ts, Tags: blkioTags},
	)

	// io_service_time_recursive and io_sectors_recursive are present on
	// cgroupsv1; absent on cgroupsv2. Emit only when data is non-empty.
	if len(s.BlkioStats.IOServiceTimeRecursive) > 0 {
		svcTotal, _, _ := blkioSplit(s.BlkioStats.IOServiceTimeRecursive)
		points = append(points,
			data_store.DataPoint{Name: "senhub.docker.blkio.service_time.total", Value: float64(svcTotal), Timestamp: ts, Tags: blkioTags},
		)
	}
	if len(s.BlkioStats.IOSectorsRecursive) > 0 {
		secTotal, _, _ := blkioSplit(s.BlkioStats.IOSectorsRecursive)
		points = append(points,
			data_store.DataPoint{Name: "senhub.docker.blkio.sectors.total", Value: float64(secTotal), Timestamp: ts, Tags: blkioTags},
		)
	}

	return points
}

// blkioSplit returns (total, read, write) from a blkio recursive entry array.
// The "Total" entry (cgroupsv1 convenience sum) is preferred for total; when
// absent (cgroupsv2), total is derived as read+write. Works for
// io_service_bytes_recursive, io_service_time_recursive, and
// io_sectors_recursive which all share the same {op, value} shape.
func blkioSplit(entries []blkioEntry) (total, read, write uint64) {
	var blkTotal uint64
	var hasTotal bool
	for _, e := range entries {
		switch strings.ToLower(e.Op) {
		case "total":
			blkTotal += e.Value
			hasTotal = true
		case "read":
			read += e.Value
		case "write":
			write += e.Value
		}
	}
	if hasTotal {
		total = blkTotal
	} else {
		total = read + write
	}
	return total, read, write
}

// primaryName returns the container's primary name with the leading '/'
// stripped. Docker reports names as "/myapp"; the slash is an artefact of
// the default bridge namespace and has no semantic value in metric tags.
func primaryName(c containerListItem) string {
	if len(c.Names) == 0 {
		return shortID(c.ID)
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

// shortID returns the first 12 characters of the Docker container ID — the
// conventional "short ID" used in docker ps output. The full 64-char ID
// is preserved as the cache discriminant; the short form keeps log lines
// readable.
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
