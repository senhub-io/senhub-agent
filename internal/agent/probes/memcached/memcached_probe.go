// Package memcached implements the free memcached probe: stats collection
// from a Memcached server over the TCP text protocol.
package memcached

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "memcached"

const (
	defaultHost     = "localhost"
	defaultPort     = 11211
	defaultTimeout  = 5 * time.Second
	defaultInterval = 60 * time.Second
)

// MemcachedProbe collects stats from a Memcached server.
type MemcachedProbe struct {
	*types.BaseProbe
	cfg          memcachedConfig
	moduleLogger *logger.ModuleLogger
	entitySrc    *memcachedEntitySource
}

type memcachedConfig struct {
	Host         string
	Port         int
	Interval     time.Duration
	Timeout      time.Duration
	InstanceName string
}

// NewMemcachedProbe constructs the probe from the YAML params block.
func NewMemcachedProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.memcached")

	cfg := memcachedConfig{
		Host:     defaultHost,
		Port:     defaultPort,
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
	}

	if v, ok := config["host"].(string); ok && v != "" {
		cfg.Host = v
	}
	if v, ok := config["port"].(int); ok && v > 0 {
		cfg.Port = v
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	probe := &MemcachedProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		entitySrc:    newMemcachedEntitySource(cfg.Host, cfg.Port, cfg.InstanceName),
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySrc)
	return probe, nil
}

func (p *MemcachedProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *MemcachedProbe) ShouldStart() bool          { return true }
func (p *MemcachedProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *MemcachedProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("host", p.cfg.Host).
		Int("port", p.cfg.Port).
		Msg("Starting memcached probe")
	return nil
}

func (p *MemcachedProbe) OnShutdown(_ context.Context) error {
	return nil
}

// Collect connects to Memcached, sends "stats\r\n", parses the response
// and emits datapoints. senhub.memcached.up is always emitted.
func (p *MemcachedProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)

	statsMap, err := p.fetchStats(addr)

	upValue := float64(1)
	if err != nil {
		upValue = 0
		p.moduleLogger.Warn().Err(err).Str("addr", addr).Msg("memcached stats fetch failed")
		p.entitySrc.setReachable(false, "")
	} else {
		p.entitySrc.setReachable(true, statsMap["version"])
	}

	commonTags := []tags.Tag{
		{Key: "instance", Value: addr},
	}

	points := []data_store.DataPoint{
		{
			Name:      "senhub.memcached.up",
			Value:     upValue,
			Timestamp: now,
			Tags:      append(commonTags, tags.Tag{Key: "metric_type", Value: "status"}),
		},
	}

	if err == nil {
		points = append(points, p.buildDataPoints(statsMap, now, commonTags)...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// fetchStats dials Memcached, sends "stats\r\n", and returns a map of
// key→value from the STAT lines.
func (p *MemcachedProbe) fetchStats(addr string) (map[string]string, error) {
	conn, err := net.DialTimeout("tcp", addr, p.cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(p.cfg.Timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("setting deadline: %w", err)
	}

	if _, err := fmt.Fprint(conn, "stats\r\n"); err != nil {
		return nil, fmt.Errorf("sending stats command: %w", err)
	}

	result := make(map[string]string)
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "END" {
			break
		}
		// Lines are: STAT <key> <value>
		parts := strings.Fields(line)
		if len(parts) == 3 && parts[0] == "STAT" {
			result[parts[1]] = parts[2]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading stats response: %w", err)
	}
	return result, nil
}

// buildDataPoints converts the raw stats map to DataPoint slices.
func (p *MemcachedProbe) buildDataPoints(stats map[string]string, now time.Time, commonTags []tags.Tag) []data_store.DataPoint {
	var points []data_store.DataPoint

	addGauge := func(name, statKey, metricType string) {
		val, ok := parseInt(stats, statKey)
		if !ok {
			return
		}
		points = append(points, data_store.DataPoint{
			Name:      name,
			Value:     float64(val),
			Timestamp: now,
			Tags:      append(commonTags, tags.Tag{Key: "metric_type", Value: metricType}),
		})
	}

	addCounter := func(name, statKey, metricType string) {
		val, ok := parseInt(stats, statKey)
		if !ok {
			return
		}
		points = append(points, data_store.DataPoint{
			Name:      name,
			Value:     float64(val),
			Timestamp: now,
			Tags:      append(commonTags, tags.Tag{Key: "metric_type", Value: metricType}),
		})
	}

	addCounterFloat := func(name, statKey, metricType string) {
		val, ok := parseFloat(stats, statKey)
		if !ok {
			return
		}
		points = append(points, data_store.DataPoint{
			Name:      name,
			Value:     float64(val),
			Timestamp: now,
			Tags:      append(commonTags, tags.Tag{Key: "metric_type", Value: metricType}),
		})
	}

	addTagged := func(name, statKey, metricType, tagKey, tagVal string) {
		val, ok := parseInt(stats, statKey)
		if !ok {
			return
		}
		t := append(commonTags,
			tags.Tag{Key: "metric_type", Value: metricType},
			tags.Tag{Key: tagKey, Value: tagVal},
		)
		points = append(points, data_store.DataPoint{
			Name:      name,
			Value:     float64(val),
			Timestamp: now,
			Tags:      t,
		})
	}

	addTaggedFloat := func(name, statKey, metricType, tagKey, tagVal string) {
		val, ok := parseFloat(stats, statKey)
		if !ok {
			return
		}
		t := append(commonTags,
			tags.Tag{Key: "metric_type", Value: metricType},
			tags.Tag{Key: tagKey, Value: tagVal},
		)
		points = append(points, data_store.DataPoint{
			Name:      name,
			Value:     float64(val),
			Timestamp: now,
			Tags:      t,
		})
	}

	// Uptime
	addCounter("memcached.uptime", "uptime", "status")

	// Connections
	addGauge("memcached.current.connections", "curr_connections", "connections")
	addCounter("memcached.connections.total", "total_connections", "connections")

	// Items
	addGauge("memcached.current.items", "curr_items", "cache")
	addCounter("memcached.items.total", "total_items", "cache")

	// Memory
	addGauge("memcached.bytes", "bytes", "memory")
	addGauge("memcached.limit_maxbytes", "limit_maxbytes", "memory")

	// Network — single metric collapsed by direction (matches otelcol-contrib
	// memcachedreceiver memcached.network{direction=transmit|receive}).
	// Tag key "direction" maps to OTel attribute "network.io.direction" via tag_to_attribute.
	addTagged("memcached.network", "bytes_written", "throughput", "direction", "transmit")
	addTagged("memcached.network", "bytes_read", "throughput", "direction", "receive")

	// Operations (get_hits / get_misses) — discriminated by "result"
	addTagged("memcached.operations", "get_hits", "cache", "result", "hit")
	addTagged("memcached.operations", "get_misses", "cache", "result", "miss")

	// Commands — discriminated by "command"
	addTagged("memcached.commands", "cmd_get", "cache", "command", "get")
	addTagged("memcached.commands", "cmd_set", "cache", "command", "set")
	addTagged("memcached.commands", "cmd_flush", "cache", "command", "flush")

	// Evictions
	addCounter("memcached.evictions", "evictions", "cache")

	// CPU — discriminated by "state"
	addTaggedFloat("memcached.cpu.usage", "rusage_user", "cpu", "state", "user")
	addTaggedFloat("memcached.cpu.usage", "rusage_system", "cpu", "state", "system")

	// Suppress unused warnings — addCounterFloat is an alias
	_ = addCounterFloat

	return points
}

func parseInt(stats map[string]string, key string) (int64, bool) {
	raw, ok := stats[key]
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseFloat(stats map[string]string, key string) (float64, bool) {
	raw, ok := stats[key]
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
