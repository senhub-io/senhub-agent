// Package zookeeper implements the free zookeeper probe: monitors an
// Apache ZooKeeper node via the mntr four-letter command over raw TCP.
// The probe connects to host:port, sends "mntr\n", reads key-tab-value
// lines until EOF, and emits OTel-named metrics covering latency,
// connections, requests, data, file descriptors, and ensemble state.
package zookeeper

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in YAML config and
// the licence catalogue.
const ProbeType = "zookeeper"

const (
	defaultHost     = "localhost"
	defaultPort     = 2181
	defaultTimeout  = 10 * time.Second
	defaultInterval = 30 * time.Second
)

type probeConfig struct {
	Host         string
	Port         int
	Timeout      time.Duration
	Interval     time.Duration
	InstanceName string
}

// ZookeeperProbe monitors a single ZooKeeper node.
type ZookeeperProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	// dial is injectable for tests; matches net.DialTimeout signature.
	dial func(network, address string, timeout time.Duration) (net.Conn, error)

	entityObs              entityObserver
	unregisterEntitySource func()
}

// fetchConf sends the "conf" four-letter-word to ZooKeeper and returns the
// parsed key=value map. ZooKeeper 3.5+ requires the conf command to be
// explicitly listed in zookeeper.4lw.commands.whitelist (or *). Parsing is
// lenient: only "key=value" lines are retained; anything else is skipped.
func (p *ZookeeperProbe) fetchConf(address string) (map[string]string, error) {
	conn, err := p.dial("tcp", address, p.cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", address, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(p.cfg.Timeout)); err != nil {
		return nil, fmt.Errorf("setting deadline: %w", err)
	}

	if _, err := fmt.Fprintf(conn, "conf\n"); err != nil {
		return nil, fmt.Errorf("sending conf: %w", err)
	}

	kv := make(map[string]string)
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		kv[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading conf response: %w", err)
	}
	return kv, nil
}

// NewZookeeperProbe constructs the probe from the free-form YAML params block.
func NewZookeeperProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.zookeeper")

	cfg := probeConfig{
		Host:     defaultHost,
		Port:     defaultPort,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	if v, ok := config["host"].(string); ok && v != "" {
		cfg.Host = v
	}
	if v, ok := config["port"].(int); ok && v > 0 {
		cfg.Port = v
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	p := &ZookeeperProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		dial:         net.DialTimeout,
	}
	p.SetProbeType(ProbeType)

	// Initialise the entity observer. If instance_name is provided it is
	// pinned immediately (precedence 1); otherwise id resolution is deferred
	// to the first successful Collect.
	p.entityObs.addr = cfg.Host
	p.entityObs.port = cfg.Port
	p.entityObs.hostIDFunc = hostID
	if cfg.InstanceName != "" {
		p.entityObs.pin(cfg.InstanceName)
	}

	return p, nil
}

// getHostIdentity is a thin wrapper around common.GetHostIdentity that
// returns the host's stable machine id as a string. It is a package-level var
// so tests can replace it without touching gopsutil.
var getHostIdentity = func() string {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return ""
	}
	return hi.ID
}

// hostID is the default hostIDFunc injected into entityObserver. It calls
// getHostIdentity so tests can override getHostIdentity and thereby control
// the fallback without reaching directly into the entityObserver struct.
func hostID() string {
	return getHostIdentity()
}

func (p *ZookeeperProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *ZookeeperProbe) ShouldStart() bool          { return true }
func (p *ZookeeperProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *ZookeeperProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntitySource = entity.RegisterSource(&p.entityObs)
	p.moduleLogger.Info().
		Str("host", p.cfg.Host).
		Int("port", p.cfg.Port).
		Msg("Starting zookeeper probe")
	return nil
}

func (p *ZookeeperProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
	}
	return nil
}

// Collect sends "mntr" to the ZooKeeper node and parses the response.
// A connection failure emits senhub.zookeeper.up=0 and returns nil —
// the probe is never in error from the scheduler's perspective; an
// unreachable node is a measured fact.
func (p *ZookeeperProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	address := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)

	kv, err := p.fetchMntr(address)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("address", address).Msg("zookeeper mntr failed")
		p.entityObs.setUp("", false, "")
		pts := []data_store.DataPoint{p.upPoint(0, now)}
		return p.BaseProbe.EnrichDataPointsWithProbeName(pts, p.GetName()), nil
	}

	version := kv["zk_version"]

	// Resolve the tech-reported server id (precedence 2) from the ZooKeeper
	// "conf" 4lw command. We attempt this only when the id has not been pinned
	// yet (instance_name already covers precedence 1). A failure is non-fatal:
	// setUp falls back to the host-derived id when serverID is empty.
	serverID := ""
	if p.entityObs.needsPin() {
		serverID = p.resolveServerID(address)
	}

	p.entityObs.setUp(serverID, true, version)
	pts := p.buildDataPoints(kv, now)
	return p.BaseProbe.EnrichDataPointsWithProbeName(pts, p.GetName()), nil
}

// resolveServerID fetches the ZooKeeper "conf" output and extracts the
// "serverId" field. Returns "zookeeper:<n>" on success, "" on any failure.
func (p *ZookeeperProbe) resolveServerID(address string) string {
	conf, err := p.fetchConf(address)
	if err != nil {
		p.moduleLogger.Debug().Err(err).Str("address", address).Msg("zookeeper conf unavailable; using fallback id")
		return ""
	}
	raw, ok := conf["serverId"]
	if !ok || strings.TrimSpace(raw) == "" {
		return ""
	}
	// Validate: serverId must be a positive integer.
	if _, err := strconv.Atoi(strings.TrimSpace(raw)); err != nil {
		return ""
	}
	return "zookeeper:" + strings.TrimSpace(raw)
}

// fetchMntr dials the ZooKeeper node, sends "mntr\n", and returns the
// parsed key→value map.
func (p *ZookeeperProbe) fetchMntr(address string) (map[string]string, error) {
	conn, err := p.dial("tcp", address, p.cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", address, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(p.cfg.Timeout)); err != nil {
		return nil, fmt.Errorf("setting deadline: %w", err)
	}

	if _, err := fmt.Fprintf(conn, "mntr\n"); err != nil {
		return nil, fmt.Errorf("sending mntr: %w", err)
	}

	kv := make(map[string]string)
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, val, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		kv[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading mntr response: %w", err)
	}
	return kv, nil
}

// buildDataPoints converts the mntr key→value map into DataPoints.
func (p *ZookeeperProbe) buildDataPoints(kv map[string]string, ts time.Time) []data_store.DataPoint {
	pts := []data_store.DataPoint{p.upPoint(1, ts)}

	add := func(name string, metricType string, v float64) {
		pts = append(pts, data_store.DataPoint{
			Name:      name,
			Value:     v,
			Timestamp: ts,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: metricType},
			},
		})
	}

	addIf := func(name, metricType, key string) {
		if raw, ok := kv[key]; ok {
			if v, err := parseFloat(raw); err == nil {
				add(name, metricType, v)
			}
		}
	}

	// Latency (ms)
	addIf("zookeeper.latency.avg", "latency", "zk_avg_latency")
	addIf("zookeeper.latency.max", "latency", "zk_max_latency")
	addIf("zookeeper.latency.min", "latency", "zk_min_latency")

	// Packets (counters)
	addIf("zookeeper.packets.received", "operations", "zk_packets_received")
	addIf("zookeeper.packets.sent", "operations", "zk_packets_sent")

	// Connections / requests
	addIf("zookeeper.connections", "connections", "zk_num_alive_connections")
	addIf("zookeeper.outstanding_requests", "requests", "zk_outstanding_requests")

	// Data
	addIf("zookeeper.znodes", "data", "zk_znode_count")
	addIf("zookeeper.watches", "data", "zk_watch_count")
	addIf("zookeeper.ephemerals", "data", "zk_ephemerals_count")
	addIf("zookeeper.data_size", "data", "zk_approximate_data_size")

	// File descriptors
	addIf("zookeeper.file_descriptors.open", "resources", "zk_open_file_descriptor_count")
	addIf("zookeeper.file_descriptors.max", "resources", "zk_max_file_descriptor_count")

	// Leader-only metrics (emitted only when present in the response)
	addIf("zookeeper.followers", "ensemble", "zk_followers")
	addIf("zookeeper.synced_followers", "ensemble", "zk_synced_followers")
	addIf("zookeeper.pending_syncs", "ensemble", "zk_pending_syncs")

	// Server state — label metric: value=1, state attribute carries the role
	if state, ok := kv["zk_server_state"]; ok && state != "" {
		pts = append(pts, data_store.DataPoint{
			Name:      "zookeeper.server_state",
			Value:     1,
			Timestamp: ts,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "ensemble"},
				{Key: "state", Value: state},
			},
		})
	}

	return pts
}

func (p *ZookeeperProbe) upPoint(v float64, ts time.Time) data_store.DataPoint {
	return data_store.DataPoint{
		Name:      "senhub.zookeeper.up",
		Value:     v,
		Timestamp: ts,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "availability"},
		},
	}
}

func parseFloat(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return 0, err
	}
	return float64(v), nil
}
