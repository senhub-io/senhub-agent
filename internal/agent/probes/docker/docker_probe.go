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
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
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
	unregister   func()
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

// containerStats is the shape of GET /containers/{id}/stats?stream=false.
type containerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		TxBytes   uint64 `json:"tx_bytes"`
		RxBytes   uint64 `json:"rx_bytes"`
		TxPackets uint64 `json:"tx_packets"`
		RxPackets uint64 `json:"rx_packets"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
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
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *dockerProbe) OnShutdown(ctx context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
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

	up := float32(0)
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
		{Name: "container.restarts", Value: float32(res.container.RestartCount), Timestamp: ts, Tags: statusTags},
	}

	if res.stats == nil {
		// Stopped or disappeared container: emit status metrics only.
		return points
	}

	s := res.stats

	// CPU metrics.
	cpuTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "cpu"})
	cpuOnline := len(s.CPUStats.CPUUsage.PercpuUsage)
	points = append(points,
		data_store.DataPoint{Name: "container.cpu.usage.total", Value: float32(s.CPUStats.CPUUsage.TotalUsage), Timestamp: ts, Tags: cpuTags},
		data_store.DataPoint{Name: "senhub.docker.cpu.system", Value: float32(s.CPUStats.SystemCPUUsage), Timestamp: ts, Tags: cpuTags},
		data_store.DataPoint{Name: "senhub.docker.cpu.online", Value: float32(cpuOnline), Timestamp: ts, Tags: cpuTags},
	)

	// Memory metrics.
	memTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "memory"})
	points = append(points,
		data_store.DataPoint{Name: "container.memory.usage", Value: float32(s.MemoryStats.Usage), Timestamp: ts, Tags: memTags},
		data_store.DataPoint{Name: "senhub.docker.memory.limit", Value: float32(s.MemoryStats.Limit), Timestamp: ts, Tags: memTags},
	)

	// Network metrics — absent for --network=host containers (nil map).
	if len(s.Networks) > 0 {
		var txBytes, rxBytes, txPkts, rxPkts uint64
		for _, iface := range s.Networks {
			txBytes += iface.TxBytes
			rxBytes += iface.RxBytes
			txPkts += iface.TxPackets
			rxPkts += iface.RxPackets
		}
		netTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "network"})
		points = append(points,
			data_store.DataPoint{Name: "container.network.io.usage.tx_bytes", Value: float32(txBytes), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "container.network.io.usage.rx_bytes", Value: float32(rxBytes), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "senhub.docker.network.tx_packets", Value: float32(txPkts), Timestamp: ts, Tags: netTags},
			data_store.DataPoint{Name: "senhub.docker.network.rx_packets", Value: float32(rxPkts), Timestamp: ts, Tags: netTags},
		)
	}

	// Block I/O metrics — op="Total" is the canonical sum on cgroupsv1; fall
	// back to summing Read+Write when Total is absent (cgroupsv2 path).
	blkTotal := blkioTotal(s)
	blkioTags := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "metric_type", Value: "blkio"})
	points = append(points,
		data_store.DataPoint{Name: "container.blockio.usage.total", Value: float32(blkTotal), Timestamp: ts, Tags: blkioTags},
	)

	return points
}

// blkioTotal sums block I/O bytes across all operations. It prefers the
// "Total" entry (cgroupsv1 convenience sum). When absent it sums "Read" and
// "Write" entries (cgroupsv2 emits those without a "Total" row).
func blkioTotal(s *containerStats) uint64 {
	var total, read, write uint64
	var hasTotal bool
	for _, e := range s.BlkioStats.IOServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "total":
			total += e.Value
			hasTotal = true
		case "read":
			read += e.Value
		case "write":
			write += e.Value
		}
	}
	if hasTotal {
		return total
	}
	return read + write
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
