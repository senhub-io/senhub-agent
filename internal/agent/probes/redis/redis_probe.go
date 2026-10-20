// Package redis implements the paid (Pro) redis probe: Redis / Valkey
// monitoring via the native RESP protocol with no external dependency.
// The only transport is raw TCP (optionally TLS); the probe sends AUTH
// + INFO all and parses the returned key-value sections.
//
// Package name "redis" is safe because the probe carries no import of
// a redis client library — stdlib only.
package redis

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

const ProbeType = "redis"

const (
	defaultTimeout  = 5 * time.Second
	defaultInterval = 60 * time.Second
)

type redisProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	tlsConfig    *tls.Config
	instance     string
	moduleLogger *logger.ModuleLogger
	entityObs    *entityObserver

	// dialFn is overridable in tests; defaults to net.DialTimeout.
	dialFn func(network, address string, timeout time.Duration) (net.Conn, error)

	// sentinelMastersFailed is true while SENTINEL MASTERS has been failing,
	// so the warning is logged only when the failure state changes.
	sentinelMastersFailed bool
}

// NewRedisProbe constructs the probe from its raw params block.
func NewRedisProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	var tlsConfig *tls.Config
	if cfg.TLS {
		tlsConfig, err = cfg.tlsClientConfig()
		if err != nil {
			return nil, err
		}
	}

	instance := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.redis")

	probe := &redisProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		tlsConfig:    tlsConfig,
		instance:     instance,
		moduleLogger: moduleLogger,
		entityObs:    newEntityObserver(cfg, dbcommon.HostID),
		dialFn:       net.DialTimeout,
	}
	probe.SetProbeType(ProbeType)
	// Redis is a remote db entity, not host-local: declare the real source
	// so the poller registers it on Start.
	probe.SetEntitySource(probe.entityObs)
	return probe, nil
}

func (p *redisProbe) ShouldStart() bool          { return true }
func (p *redisProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *redisProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().Str("instance", p.instance).Msg("Redis probe started")
	return nil
}

func (p *redisProbe) OnShutdown(_ context.Context) error {
	return nil
}

// handshake runs the TLS handshake now. crypto/tls otherwise performs it on
// the first write, so a certificate the agent does not trust, or a server
// that speaks plain Redis, surfaced as a failure of the first command
// ("AUTH send failed") that names nothing about TLS.
func (p *redisProbe) handshake(conn net.Conn) (net.Conn, error) {
	tlsConn := tls.Client(conn, p.tlsConfig)
	if err := tlsConn.SetDeadline(time.Now().Add(p.cfg.Timeout)); err != nil {
		return nil, fmt.Errorf("TLS handshake with %s failed: %w", p.instance, err)
	}
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("TLS handshake with %s failed: %w", p.instance, err)
	}
	if err := tlsConn.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("TLS handshake with %s failed: %w", p.instance, err)
	}
	return tlsConn, nil
}

// authCommandArgs returns the RESP AUTH command parts for the active config.
// With no password there is no AUTH command; with a username Redis expects
// AUTH <username> <password>, while legacy password-only auth is unchanged.
func authCommandArgs(cfg probeConfig) []string {
	if cfg.Password == "" {
		return nil
	}
	if cfg.Username != "" {
		return []string{"AUTH", cfg.Username, cfg.Password}
	}
	return []string{"AUTH", cfg.Password}
}

// Collect dials the Redis server, authenticates if configured, issues
// INFO all, INFO commandstats, and conditionally INFO cluster / INFO sentinel,
// parses the results and emits OTel-canonical datapoints. A connection or
// auth failure is a measurement (senhub.db.up=0), not a collection error.
func (p *redisProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	conn, err := p.dialFn("tcp", p.instance, p.cfg.Timeout)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis connect failed")
		return p.BaseProbe.EnrichDataPointsWithProbeName(p.failurePoints(err, now), p.GetName()), nil
	}
	defer conn.Close()

	if p.cfg.TLS {
		tlsConn, err := p.handshake(conn)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis TLS handshake failed")
			return p.BaseProbe.EnrichDataPointsWithProbeName(p.failurePoints(err, now), p.GetName()), nil
		}
		conn = tlsConn
	}

	if parts := authCommandArgs(p.cfg); len(parts) > 0 {
		if err := sendCommand(conn, p.cfg.Timeout, parts...); err != nil {
			p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis AUTH send failed")
			return p.BaseProbe.EnrichDataPointsWithProbeName(p.failurePoints(err, now), p.GetName()), nil
		}
		resp, err := readLine(conn, p.cfg.Timeout)
		if err != nil || strings.HasPrefix(resp, "-") {
			if err == nil {
				err = fmt.Errorf("auth rejected: %s", resp)
			}
			p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis AUTH failed")
			return p.BaseProbe.EnrichDataPointsWithProbeName(p.failurePoints(err, now), p.GetName()), nil
		}
	}

	if err := sendCommand(conn, p.cfg.Timeout, "INFO", "all"); err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO send failed")
		return p.BaseProbe.EnrichDataPointsWithProbeName(p.failurePoints(err, now), p.GetName()), nil
	}

	blob, err := readInfo(conn, p.cfg.Timeout)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO read failed")
		return p.BaseProbe.EnrichDataPointsWithProbeName(p.failurePoints(err, now), p.GetName()), nil
	}

	info := parseInfoBlob(blob)

	// Second command on the same connection: INFO commandstats.
	var cmdStats map[string]cmdStat
	if err := sendCommand(conn, p.cfg.Timeout, "INFO", "commandstats"); err == nil {
		if csBlob, err := readInfo(conn, p.cfg.Timeout); err == nil {
			cmdStats = parseCommandStats(csBlob)
		} else {
			p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO commandstats read failed")
		}
	} else {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO commandstats send failed")
	}

	// INFO cluster — only when cluster_enabled=1 in INFO server section.
	var clusterInfo map[string]string
	if info["cluster_enabled"] == "1" {
		if err := sendCommand(conn, p.cfg.Timeout, "INFO", "cluster"); err == nil {
			if cBlob, err := readInfo(conn, p.cfg.Timeout); err == nil {
				clusterInfo = parseInfoBlob(cBlob)
			} else {
				p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO cluster read failed")
			}
		} else {
			p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO cluster send failed")
		}
	}

	// INFO sentinel and SENTINEL MASTERS — only when redis_mode=sentinel in
	// INFO server section.
	var sentinelInfo map[string]string
	var sentinelMasters []sentinelMasterDetails
	if info["redis_mode"] == "sentinel" {
		if err := sendCommand(conn, p.cfg.Timeout, "INFO", "sentinel"); err == nil {
			if sBlob, err := readInfo(conn, p.cfg.Timeout); err == nil {
				sentinelInfo = parseInfoBlob(sBlob)
			} else {
				p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO sentinel read failed")
			}
		} else {
			p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO sentinel send failed")
		}

		if err := sendCommand(conn, p.cfg.Timeout, "SENTINEL", "MASTERS"); err != nil {
			p.logSentinelMastersFailure(err)
		} else {
			reply, err := readRESPValue(conn, p.cfg.Timeout)
			if err != nil {
				p.logSentinelMastersFailure(err)
			} else {
				sentinelMasters, err = parseSentinelMastersReply(reply)
				if err != nil {
					p.logSentinelMastersFailure(err)
					sentinelMasters = nil
				} else {
					p.sentinelMastersFailed = false
				}
			}
		}
	}

	points := p.buildDatapoints(info, cmdStats, clusterInfo, sentinelInfo, now, 1)
	if len(sentinelMasters) > 0 {
		p.addSentinelMasterDetailPoints(&points, sentinelMasters, now)
	}
	p.entityObs.update(p.cfg, info)
	points = append(points, data_store.DataPoint{
		Name:      dbcommon.CheckResultMetric,
		Value:     float64(dbcommon.CheckOK),
		Timestamp: now,
		Tags:      p.baseTags("status"),
	})

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *redisProbe) logSentinelMastersFailure(err error) {
	if p.sentinelMastersFailed {
		return
	}
	p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis SENTINEL MASTERS failed")
	p.sentinelMastersFailed = true
}

// baseTags returns the common tags emitted on every datapoint.
func (p *redisProbe) baseTags(metricType string) []tags.Tag {
	t := []tags.Tag{
		{Key: "instance", Value: p.instance},
		{Key: "db.system.name", Value: "redis"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
		{Key: "metric_type", Value: metricType},
	}
	// The identity of the entity these metrics describe (#741). It is pinned
	// when the probe is built (instance_name, or the host-scoped fallback
	// Redis always needs, having no server id), so every series is born with
	// it. A blank value is never emitted: a blank label and the real one would
	// be two series for one database.
	if id := p.entityObs.instanceID(); id != "" {
		t = append(t, tags.Tag{Key: "db.instance.id", Value: id})
	}
	return t
}

// failurePoints returns the up=0 and check-result datapoints emitted when
// a connection/auth/read failure makes this target unreachable.
func (p *redisProbe) failurePoints(err error, ts time.Time) []data_store.DataPoint {
	return []data_store.DataPoint{
		{Name: "senhub.db.up", Value: 0, Timestamp: ts, Tags: p.baseTags("overview")},
		{Name: dbcommon.CheckResultMetric, Value: float64(dbcommon.ClassifyError(err)), Timestamp: ts, Tags: p.baseTags("status")},
	}
}

func (p *redisProbe) addGauge(out *[]data_store.DataPoint, name string, value float64, ts time.Time, metricType string, extra ...tags.Tag) {
	t := p.baseTags(metricType)
	t = append(t, extra...)
	*out = append(*out, data_store.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: t})
}

func (p *redisProbe) addCounter(out *[]data_store.DataPoint, name string, value float64, ts time.Time, metricType string, extra ...tags.Tag) {
	p.addGauge(out, name, value, ts, metricType, extra...)
}

// cmdStat holds the per-command statistics from INFO commandstats.
type cmdStat struct {
	calls int64
	usec  int64
}

// parseCommandStats parses the INFO commandstats blob into a per-command map.
// Each line has the form:
//
//	cmdstat_get:calls=1000,usec=5000,usec_per_call=5.00,rejected_calls=0,failed_calls=0
func parseCommandStats(blob string) map[string]cmdStat {
	out := make(map[string]cmdStat)
	for _, line := range strings.Split(blob, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cmdName, fields, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		cmdName = strings.TrimPrefix(cmdName, "cmdstat_")
		if cmdName == "" {
			continue
		}
		var cs cmdStat
		for _, field := range strings.Split(fields, ",") {
			k, v, ok2 := strings.Cut(field, "=")
			if !ok2 {
				continue
			}
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				// usec_per_call is a float — skip it gracefully.
				continue
			}
			switch strings.TrimSpace(k) {
			case "calls":
				cs.calls = n
			case "usec":
				cs.usec = n
			}
		}
		out[cmdName] = cs
	}
	return out
}

// buildDatapoints converts the parsed INFO map to OTel-canonical datapoints.
// clusterInfo is the parsed INFO cluster blob (nil when cluster_enabled!=1).
// sentinelInfo is the parsed INFO sentinel blob (nil when not in sentinel mode).
func (p *redisProbe) buildDatapoints(info map[string]string, cmdStats map[string]cmdStat, clusterInfo map[string]string, sentinelInfo map[string]string, ts time.Time, up float64) []data_store.DataPoint {
	var pts []data_store.DataPoint

	p.addGauge(&pts, "senhub.db.up", up, ts, "overview")
	if up == 0 {
		return pts
	}

	p.addOverviewDatapoints(&pts, info, ts)
	p.addKeyspaceDatapoints(&pts, info, ts)
	p.addReplicationDatapoints(&pts, info, ts)
	p.addPersistenceDatapoints(&pts, info, ts)
	p.addResourceDatapoints(&pts, info, ts)
	p.addCommandStatDatapoints(&pts, cmdStats, ts)
	p.addClusterDatapoints(&pts, clusterInfo, ts)
	p.addSentinelDatapoints(&pts, sentinelInfo, ts)
	p.addTrackingDatapoints(&pts, info, ts)
	return pts
}

func (p *redisProbe) addOverviewDatapoints(pts *[]data_store.DataPoint, info map[string]string, ts time.Time) {
	// overview
	if v, ok := parseFloat(info["uptime_in_seconds"]); ok {
		p.addCounter(pts, "redis.uptime", v, ts, "overview")
	}
	if ver := info["redis_version"]; ver != "" {
		p.addGauge(pts, "senhub.db.version.info", 1, ts, "overview",
			tags.Tag{Key: "version", Value: ver})
	}

	// connections
	if v, ok := parseFloat(info["connected_clients"]); ok {
		p.addGauge(pts, "redis.clients.connected", v, ts, "connections")
	}
	if v, ok := parseFloat(info["blocked_clients"]); ok {
		p.addGauge(pts, "redis.clients.blocked", v, ts, "connections")
	}
	if v, ok := parseFloat(info["total_connections_received"]); ok {
		p.addCounter(pts, "redis.connections.received", v, ts, "connections")
	}
	if v, ok := parseFloat(info["rejected_connections"]); ok {
		p.addCounter(pts, "redis.connections.rejected", v, ts, "connections")
	}

	// memory
	if v, ok := parseFloat(info["used_memory"]); ok {
		p.addGauge(pts, "redis.memory.used", v, ts, "memory")
	}
	if v, ok := parseFloat(info["used_memory_rss"]); ok {
		p.addGauge(pts, "redis.memory.used.rss", v, ts, "memory")
	}
	if v, ok := parseFloat(info["used_memory_peak"]); ok {
		p.addGauge(pts, "redis.memory.peak", v, ts, "memory")
	}
	if v, ok := parseFloat(info["mem_fragmentation_ratio"]); ok {
		p.addGauge(pts, "redis.memory.fragmentation.ratio", v, ts, "memory")
	}

	// throughput
	if v, ok := parseFloat(info["total_commands_processed"]); ok {
		p.addCounter(pts, "redis.commands.processed", v, ts, "throughput")
	}
	if v, ok := parseFloat(info["total_net_input_bytes"]); ok {
		p.addCounter(pts, "redis.net.input", v, ts, "throughput")
	}
	if v, ok := parseFloat(info["total_net_output_bytes"]); ok {
		p.addCounter(pts, "redis.net.output", v, ts, "throughput")
	}
	if v, ok := parseFloat(info["instantaneous_ops_per_sec"]); ok {
		p.addGauge(pts, "redis.ops.per_sec", v, ts, "throughput")
	}
}

func (p *redisProbe) addKeyspaceDatapoints(pts *[]data_store.DataPoint, info map[string]string, ts time.Time) {
	// cache / keyspace hits
	hits, hitsOK := parseFloat(info["keyspace_hits"])
	misses, missesOK := parseFloat(info["keyspace_misses"])
	if hitsOK {
		p.addCounter(pts, "redis.keyspace.hits", hits, ts, "cache")
	}
	if missesOK {
		p.addCounter(pts, "redis.keyspace.misses", misses, ts, "cache")
	}
	sum := hits + misses
	ratio := float64(0)
	if sum > 0 {
		ratio = hits / sum * 100
	}
	p.addGauge(pts, "redis.keyspace.hit.ratio", ratio, ts, "cache")

	// keyspace per-db (lines like db0:keys=N,expires=M,avg_ttl=T)
	sawDB0 := false
	for key, val := range info {
		if !keyspaceDBKey.MatchString(key) {
			continue
		}
		dbIdx, keys, expires, avgTTL, ok := parseKeyspaceLine(key + ":" + val)
		if !ok {
			continue
		}
		if dbIdx == "0" {
			sawDB0 = true
		}
		dbTag := tags.Tag{Key: "db", Value: dbIdx}
		p.addGauge(pts, "redis.db.keys", float64(keys), ts, "keyspace", dbTag)
		p.addGauge(pts, "redis.db.expires", float64(expires), ts, "keyspace", dbTag)
		p.addGauge(pts, "redis.db.avg_ttl", float64(avgTTL), ts, "keyspace", dbTag)
	}
	// Redis omits the line of a database that holds no key, so an emptied
	// db0 would lose its channels instead of reading 0. A Sentinel has no
	// keyspace at all.
	if !sawDB0 && info["redis_mode"] != "sentinel" {
		dbTag := tags.Tag{Key: "db", Value: "0"}
		p.addGauge(pts, "redis.db.keys", 0, ts, "keyspace", dbTag)
		p.addGauge(pts, "redis.db.expires", 0, ts, "keyspace", dbTag)
		p.addGauge(pts, "redis.db.avg_ttl", 0, ts, "keyspace", dbTag)
	}
}

func (p *redisProbe) addReplicationDatapoints(pts *[]data_store.DataPoint, info map[string]string, ts time.Time) {
	// replication
	role := info["role"]
	roleVal := float64(-1)
	switch role {
	case "master":
		roleVal = 1
	case "slave", "replica":
		roleVal = 0
	}
	p.addGauge(pts, "redis.replication.role", roleVal, ts, "replication")

	// replication offset: master_repl_offset on master; slave_repl_offset /
	// replica_repl_offset on replica (Redis 7 renamed the field).
	replOffset := ""
	if role == "master" {
		replOffset = info["master_repl_offset"]
	} else {
		if v := info["slave_repl_offset"]; v != "" {
			replOffset = v
		}
		if v := info["replica_repl_offset"]; v != "" {
			replOffset = v
		}
	}
	if v, ok := parseFloat(replOffset); ok {
		p.addCounter(pts, "redis.replication.offset", v, ts, "replication")
	}

	if role == "master" {
		if v, ok := parseFloat(info["connected_slaves"]); ok {
			p.addGauge(pts, "redis.replication.slaves.connected", v, ts, "replication")
		}
	} else {
		if v, ok := parseFloat(info["master_last_io_seconds_ago"]); ok {
			p.addGauge(pts, "redis.replication.lag", v, ts, "replication")
		}
	}
}

func (p *redisProbe) addPersistenceDatapoints(pts *[]data_store.DataPoint, info map[string]string, ts time.Time) {
	// persistence
	if v, ok := parseFloat(info["rdb_changes_since_last_save"]); ok {
		p.addGauge(pts, "redis.rdb.changes", v, ts, "persistence")
	}
	if v, ok := parseFloat(info["aof_enabled"]); ok {
		p.addGauge(pts, "redis.aof.enabled", v, ts, "persistence")
	}
	if v, ok := parseFloat(info["rdb_last_bgsave_time_sec"]); ok {
		p.addGauge(pts, "redis.rdb.last_bgsave.duration", v, ts, "persistence")
	}
	if lastSaveStr := info["rdb_last_save_time"]; lastSaveStr != "" {
		if lastSaveEpoch, err := strconv.ParseInt(strings.TrimSpace(lastSaveStr), 10, 64); err == nil && lastSaveEpoch > 0 {
			age := ts.Unix() - lastSaveEpoch
			if age < 0 {
				age = 0
			}
			p.addGauge(pts, "redis.rdb.last_save.age", float64(age), ts, "persistence")
		}
	}

	for _, st := range []struct{ field, metric string }{
		{"rdb_last_bgsave_status", "redis.rdb.last_bgsave.status"},
		{"aof_last_bgrewrite_status", "redis.aof.last_bgrewrite.status"},
		{"aof_last_write_status", "redis.aof.last_write.status"},
	} {
		if raw, present := info[st.field]; present && raw != "" {
			p.addGauge(pts, st.metric, statusValue(raw), ts, "persistence")
		}
	}
	if v, ok := parseFloat(info["aof_last_rewrite_time_sec"]); ok {
		p.addGauge(pts, "redis.aof.last_rewrite.duration", v, ts, "persistence")
	}
}

func (p *redisProbe) addResourceDatapoints(pts *[]data_store.DataPoint, info map[string]string, ts time.Time) {
	// cpu
	for _, state := range []struct{ field, label string }{
		{"used_cpu_sys", "sys"},
		{"used_cpu_user", "user"},
		{"used_cpu_sys_children", "sys_children"},
		{"used_cpu_user_children", "user_children"},
	} {
		if v, ok := parseFloat(info[state.field]); ok {
			p.addCounter(pts, "redis.cpu.time", v, ts, "cpu",
				tags.Tag{Key: "state", Value: state.label})
		}
	}

	// memory — additional fields
	if v, ok := parseFloat(info["used_memory_lua"]); ok {
		p.addGauge(pts, "redis.memory.lua", v, ts, "memory")
	}

	// client buffers
	if v, ok := parseFloat(info["client_recent_max_input_buffer"]); ok {
		p.addGauge(pts, "redis.clients.max_input_buffer", v, ts, "connections")
	}
	if v, ok := parseFloat(info["client_recent_max_output_buffer"]); ok {
		p.addGauge(pts, "redis.clients.max_output_buffer", v, ts, "connections")
	}

	// fork duration
	if v, ok := parseFloat(info["latest_fork_usec"]); ok {
		p.addGauge(pts, "redis.latest.fork", v, ts, "persistence")
	}

	// replication backlog
	if v, ok := parseFloat(info["repl_backlog_first_byte_offset"]); ok {
		p.addGauge(pts, "redis.replication.backlog_first_byte_offset", v, ts, "replication")
	}

	if v, ok := parseFloat(info["repl_backlog_active"]); ok {
		p.addGauge(pts, "redis.replication.backlog_active", v, ts, "replication")
	}
	if v, ok := parseFloat(info["repl_backlog_size"]); ok {
		p.addGauge(pts, "redis.replication.backlog_size", v, ts, "replication")
	}
	if v, ok := parseFloat(info["repl_backlog_histlen"]); ok {
		p.addGauge(pts, "redis.replication.backlog_histlen", v, ts, "replication")
	}

	// pub/sub
	if v, ok := parseFloat(info["pubsub_channels"]); ok {
		p.addGauge(pts, "redis.pubsub.channels", v, ts, "pubsub")
	}
	if v, ok := parseFloat(info["pubsub_patterns"]); ok {
		p.addGauge(pts, "redis.pubsub.patterns", v, ts, "pubsub")
	}

	// evicted and expired keys
	if v, ok := parseFloat(info["evicted_keys"]); ok {
		p.addCounter(pts, "redis.evicted_keys", v, ts, "cache")
	}
	if v, ok := parseFloat(info["expired_keys"]); ok {
		p.addCounter(pts, "redis.expired_keys", v, ts, "cache")
	}
}

func (p *redisProbe) addCommandStatDatapoints(pts *[]data_store.DataPoint, cmdStats map[string]cmdStat, ts time.Time) {
	// per-command stats
	for cmd, cs := range cmdStats {
		cmdTag := tags.Tag{Key: "cmd", Value: cmd}
		p.addCounter(pts, "redis.cmd.calls", float64(cs.calls), ts, "commands", cmdTag)
		p.addCounter(pts, "redis.cmd.usec", float64(cs.usec), ts, "commands", cmdTag)
	}
}

func (p *redisProbe) addClusterDatapoints(pts *[]data_store.DataPoint, clusterInfo map[string]string, ts time.Time) {
	// cluster metrics (INFO cluster — only populated when cluster_enabled=1)
	if clusterInfo != nil {
		// cluster_state: "ok" → 1, anything else → 0.
		stateVal := float64(0)
		if clusterInfo["cluster_state"] == "ok" {
			stateVal = 1
		}
		p.addGauge(pts, "redis.cluster.state", stateVal, ts, "cluster")

		if v, ok := parseFloat(clusterInfo["cluster_slots_assigned"]); ok {
			p.addGauge(pts, "redis.cluster.slots.assigned", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_slots_ok"]); ok {
			p.addGauge(pts, "redis.cluster.slots.ok", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_slots_pfail"]); ok {
			p.addGauge(pts, "redis.cluster.slots.pfail", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_slots_fail"]); ok {
			p.addGauge(pts, "redis.cluster.slots.fail", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_stats_messages_sent"]); ok {
			p.addCounter(pts, "redis.cluster.links.created", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_stats_messages_received"]); ok {
			p.addCounter(pts, "redis.cluster.links.disconnected", v, ts, "cluster")
		}
	}
}

func (p *redisProbe) addSentinelDatapoints(pts *[]data_store.DataPoint, sentinelInfo map[string]string, ts time.Time) {
	// sentinel metrics (INFO sentinel — only populated in sentinel mode)
	if sentinelInfo != nil {
		if v, ok := parseFloat(sentinelInfo["sentinel_masters"]); ok {
			p.addGauge(pts, "redis.sentinel.masters", v, ts, "sentinel")
		}
		if v, ok := parseFloat(sentinelInfo["sentinel_running_scripts"]); ok {
			p.addGauge(pts, "redis.sentinel.scripts_queue_length", v, ts, "sentinel")
		}
		// Aggregate ok/total slaves and sentinels across all monitored masters.
		// Each master is reported as: sentinel_mastersN:name=...,status=...,slaves=N,sentinels=M
		// Parallel fields sentinel_slavesN_... and sentinel_sentinelsN_... provide per-master detail.
		totalSlaves, okSlaves, totalSentinels, okSentinels := parseSentinelMasterStats(sentinelInfo)
		p.addGauge(pts, "redis.sentinel.slaves", float64(totalSlaves), ts, "sentinel")
		p.addGauge(pts, "redis.sentinel.ok_slaves", float64(okSlaves), ts, "sentinel")
		p.addGauge(pts, "redis.sentinel.sentinels", float64(totalSentinels), ts, "sentinel")
		p.addGauge(pts, "redis.sentinel.ok_sentinels", float64(okSentinels), ts, "sentinel")
		for _, m := range parseSentinelMasters(sentinelInfo) {
			masterTag := tags.Tag{Key: "master", Value: m.name}
			p.addGauge(pts, "redis.sentinel.master.status", m.statusValue(), ts, "sentinel", masterTag)
			p.addGauge(pts, "redis.sentinel.master.slaves", float64(m.slaves), ts, "sentinel", masterTag)
			p.addGauge(pts, "redis.sentinel.master.sentinels", float64(m.sentinels), ts, "sentinel", masterTag)
		}
	}
}

func (p *redisProbe) addTrackingDatapoints(pts *[]data_store.DataPoint, info map[string]string, ts time.Time) {
	// tracking metrics (RESP3 client-side tracking, present in INFO clients
	// only when at least one client uses TRACKING; parse defensively).
	if v, ok := parseFloat(info["tracking_clients"]); ok {
		p.addGauge(pts, "redis.tracking.clients", v, ts, "tracking")
	}
	if v, ok := parseFloat(info["tracking_table_used_keys"]); ok {
		p.addGauge(pts, "redis.tracking.keys", v, ts, "tracking")
	}
}

// sentinelMasterDetails is one master from SENTINEL MASTERS.
type sentinelMasterDetails struct {
	name               string
	address            string
	quorum             int
	otherSentinels     int
	failoverInProgress bool
}

// addSentinelMasterDetailPoints emits the SENTINEL MASTERS-only gauges for
// each monitored master.
func (p *redisProbe) addSentinelMasterDetailPoints(pts *[]data_store.DataPoint, masters []sentinelMasterDetails, ts time.Time) {
	for _, m := range masters {
		masterTag := tags.Tag{Key: "master", Value: m.name}
		addressTag := tags.Tag{Key: "sentinel_master_address", Value: m.address}
		p.addGauge(pts, "redis.sentinel.master.quorum", float64(m.quorum), ts, "sentinel", masterTag, addressTag)
		failoverVal := float64(0)
		if m.failoverInProgress {
			failoverVal = 1
		}
		p.addGauge(pts, "redis.sentinel.master.failover_in_progress", failoverVal, ts, "sentinel", masterTag, addressTag)
		p.addGauge(pts, "redis.sentinel.master.other_sentinels", float64(m.otherSentinels), ts, "sentinel", masterTag, addressTag)
	}
}

// statusValue maps a Redis "ok"/"err" status field to 1/0.
func statusValue(raw string) float64 {
	if strings.TrimSpace(raw) == "ok" {
		return 1
	}
	return 0
}

// sentinelMaster is one monitored master from an INFO sentinel masterN line.
type sentinelMaster struct {
	name      string
	status    string
	slaves    int
	sentinels int
}

func (m sentinelMaster) statusValue() float64 { return statusValue(m.status) }

// parseSentinelMasters returns the monitored masters of an INFO sentinel map,
// ordered by their masterN index. A line without a name is skipped: the name
// is the only identity a series can carry.
func parseSentinelMasters(m map[string]string) []sentinelMaster {
	type indexed struct {
		idx int
		sentinelMaster
	}
	var found []indexed
	for k, v := range m {
		suffix, ok := strings.CutPrefix(k, "master")
		if !ok {
			continue
		}
		idx, err := strconv.Atoi(suffix)
		if err != nil || idx < 0 {
			continue
		}
		var sm sentinelMaster
		for _, field := range strings.Split(v, ",") {
			fk, fv, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			fv = strings.TrimSpace(fv)
			switch strings.TrimSpace(fk) {
			case "name":
				sm.name = fv
			case "status":
				sm.status = fv
			case "slaves":
				sm.slaves, _ = strconv.Atoi(fv)
			case "sentinels":
				sm.sentinels, _ = strconv.Atoi(fv)
			}
		}
		if sm.name == "" {
			continue
		}
		found = append(found, indexed{idx, sm})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].idx < found[j].idx })
	out := make([]sentinelMaster, 0, len(found))
	for _, f := range found {
		out = append(out, f.sentinelMaster)
	}
	return out
}

// parseSentinelMasterStats sums slaves and sentinels across all monitored
// masters from the flat INFO sentinel map. Redis reports per-master lines:
//
//	master0:name=mymaster,status=ok,address=127.0.0.1:6379,slaves=1,sentinels=2
//
// Returns (totalSlaves, okSlaves, totalSentinels, okSentinels).
func parseSentinelMasterStats(m map[string]string) (totalSlaves, okSlaves, totalSentinels, okSentinels int) {
	for k, v := range m {
		if !strings.HasPrefix(k, "master") {
			continue
		}
		// k is e.g. "master0", "master1".
		// Check it looks like a numeric master index by verifying the suffix is numeric.
		suffix := strings.TrimPrefix(k, "master")
		if suffix == "" {
			continue
		}
		allDigits := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if !allDigits {
			continue
		}

		// v = "name=mymaster,status=ok,address=127.0.0.1:6379,slaves=1,sentinels=2"
		isOK := false
		slaves := 0
		sentinels := 0
		for _, field := range strings.Split(v, ",") {
			fk, fv, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(fk) {
			case "status":
				isOK = strings.TrimSpace(fv) == "ok"
			case "slaves":
				if n, err := strconv.Atoi(strings.TrimSpace(fv)); err == nil {
					slaves = n
				}
			case "sentinels":
				if n, err := strconv.Atoi(strings.TrimSpace(fv)); err == nil {
					sentinels = n
				}
			}
		}
		totalSlaves += slaves
		totalSentinels += sentinels
		if isOK {
			okSlaves += slaves
			okSentinels += sentinels
		}
	}
	return
}

// parseKeyspaceLine parses a keyspace entry. The INFO map stores the
// value without the key prefix, so the caller must pass "db0:keys=N,...".
// Returns (dbIndex, keys, expires, avgTTL, ok).
// keyspaceDBKey matches a logical database line of INFO keyspace. Redis 8
// also prints keysizes lines such as db0_distrib_strings_sizes, which are
// not databases.
var keyspaceDBKey = regexp.MustCompile(`^db[0-9]+$`)

func parseKeyspaceLine(line string) (dbIdx string, keys int64, expires int64, avgTTL int64, ok bool) {
	// line = "db0:keys=100,expires=5,avg_ttl=3000"
	dbPart, rest, cut := strings.Cut(line, ":")
	if !cut {
		return "", 0, 0, 0, false
	}
	if !keyspaceDBKey.MatchString(dbPart) {
		return "", 0, 0, 0, false
	}
	dbIdx = strings.TrimPrefix(dbPart, "db")

	for _, field := range strings.Split(rest, ",") {
		k, v, ok2 := strings.Cut(field, "=")
		if !ok2 {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(k) {
		case "keys":
			keys = n
		case "expires":
			expires = n
		case "avg_ttl":
			avgTTL = n
		}
	}
	return dbIdx, keys, expires, avgTTL, true
}

// parseInfoBlob parses the raw multi-section INFO blob into a flat
// key→value map. Section headers (# Server etc.) and blank lines are
// skipped. Keyspace lines are stored by their full value so the
// per-db parser sees "keys=N,expires=M,avg_ttl=T".
func parseInfoBlob(blob string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(blob, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// sendCommand writes a RESP array command to the connection. Using the
// array form (not inline) correctly encodes passwords with spaces or
// special characters.
func sendCommand(conn net.Conn, timeout time.Duration, parts ...string) error {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	w := bufio.NewWriter(conn)
	fmt.Fprintf(w, "*%d\r\n", len(parts))
	for _, p := range parts {
		fmt.Fprintf(w, "$%d\r\n%s\r\n", len(p), p)
	}
	return w.Flush()
}

// readLine reads one RESP line (terminated by \r\n) from the connection.
func readLine(conn net.Conn, timeout time.Duration) (string, error) {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readInfo reads the RESP bulk string response to INFO all.
// bufio.Scanner cannot be used here: it strips \r\n terminators and
// would mangle the binary bulk string length header, truncating the
// INFO body at its first empty section separator.
func readInfo(conn net.Conn, timeout time.Duration) (string, error) {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	r := bufio.NewReader(conn)

	header, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading INFO header: %w", err)
	}
	header = strings.TrimRight(header, "\r\n")

	if !strings.HasPrefix(header, "$") {
		return "", fmt.Errorf("unexpected INFO response: %s", header)
	}
	n, err := strconv.Atoi(header[1:])
	if err != nil || n < 0 {
		return "", fmt.Errorf("invalid INFO bulk length %q: %w", header, err)
	}

	// Read exactly n bytes then the trailing \r\n.
	body := make([]byte, n)
	if _, err := readFull(r, body); err != nil {
		return "", fmt.Errorf("reading INFO body: %w", err)
	}
	// Consume trailing \r\n.
	if _, err := r.ReadString('\n'); err != nil {
		return "", fmt.Errorf("reading INFO trailing CRLF: %w", err)
	}
	return string(body), nil
}

// respValue is a small RESP value container used for nested replies such
// as SENTINEL MASTERS.
type respValue struct {
	typ byte
	str string
	arr []respValue
}

// readRESPValue reads one RESP value from the connection.
func readRESPValue(conn net.Conn, timeout time.Duration) (respValue, error) {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return respValue{}, fmt.Errorf("setting RESP deadline: %w", err)
	}
	v, err := readRESP(bufio.NewReader(conn))
	if err != nil {
		return respValue{}, err
	}
	return v, nil
}

// readRESP reads one RESP value from a buffered reader. It supports simple
// strings, errors, integers, bulk strings and nested arrays.
func readRESP(r *bufio.Reader) (respValue, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return respValue{}, fmt.Errorf("reading RESP line: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return respValue{}, fmt.Errorf("empty RESP response")
	}

	switch line[0] {
	case '+':
		return respValue{typ: '+', str: line[1:]}, nil
	case '-':
		return respValue{typ: '-', str: line[1:]}, nil
	case ':':
		if _, err := strconv.ParseInt(line[1:], 10, 64); err != nil {
			return respValue{}, fmt.Errorf("invalid RESP integer %q: %w", line, err)
		}
		return respValue{typ: ':', str: line[1:]}, nil
	case '$':
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return respValue{}, fmt.Errorf("invalid RESP bulk length %q: %w", line, err)
		}
		if n < 0 {
			return respValue{typ: '$'}, nil
		}
		body := make([]byte, n)
		if _, err := readFull(r, body); err != nil {
			return respValue{}, fmt.Errorf("reading RESP bulk body: %w", err)
		}
		if _, err := r.ReadString('\n'); err != nil {
			return respValue{}, fmt.Errorf("reading RESP bulk trailing CRLF: %w", err)
		}
		return respValue{typ: '$', str: string(body)}, nil
	case '*':
		count, err := strconv.Atoi(line[1:])
		if err != nil {
			return respValue{}, fmt.Errorf("invalid RESP array length %q: %w", line, err)
		}
		if count < 0 {
			return respValue{typ: '*'}, nil
		}
		arr := make([]respValue, 0, count)
		for i := 0; i < count; i++ {
			v, err := readRESP(r)
			if err != nil {
				return respValue{}, err
			}
			arr = append(arr, v)
		}
		return respValue{typ: '*', arr: arr}, nil
	default:
		return respValue{}, fmt.Errorf("unknown RESP type %q", line)
	}
}

// parseSentinelMastersReply parses the nested RESP array returned by
// SENTINEL MASTERS into sentinelMasterDetails values.
func parseSentinelMastersReply(v respValue) ([]sentinelMasterDetails, error) {
	if v.typ == '-' {
		return nil, fmt.Errorf("SENTINEL MASTERS reply error: %s", v.str)
	}
	if v.typ != '*' {
		return nil, fmt.Errorf("SENTINEL MASTERS reply must be an array, got RESP type %q", v.typ)
	}

	out := make([]sentinelMasterDetails, 0, len(v.arr))
	for _, entry := range v.arr {
		m, ok, err := parseSentinelMasterDetail(entry)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func parseSentinelMasterDetail(v respValue) (sentinelMasterDetails, bool, error) {
	if v.typ != '*' {
		return sentinelMasterDetails{}, false, fmt.Errorf("SENTINEL MASTERS master entry must be an array, got RESP type %q", v.typ)
	}
	if len(v.arr)%2 != 0 {
		return sentinelMasterDetails{}, false, fmt.Errorf("SENTINEL MASTERS master entry has odd field/value count %d", len(v.arr))
	}

	var m sentinelMasterDetails
	var ip, port, flags, failoverState string
	for i := 0; i < len(v.arr); i += 2 {
		key := v.arr[i].str
		val := v.arr[i+1].str
		switch key {
		case "name":
			m.name = val
		case "ip":
			ip = val
		case "port":
			port = val
		case "flags":
			flags = val
		case "quorum":
			m.quorum, _ = strconv.Atoi(val)
		case "num-other-sentinels":
			m.otherSentinels, _ = strconv.Atoi(val)
		case "failover-state":
			failoverState = val
		}
	}

	if m.name == "" {
		return sentinelMasterDetails{}, false, nil
	}
	if ip != "" && port != "" {
		m.address = net.JoinHostPort(ip, port)
	}
	m.failoverInProgress = flagsContain(flags, "failover_in_progress") || (failoverState != "" && failoverState != "none")
	return m, true, nil
}

func flagsContain(flags, flag string) bool {
	for _, f := range strings.Split(flags, ",") {
		if strings.TrimSpace(f) == flag {
			return true
		}
	}
	return false
}

// readFull reads exactly len(buf) bytes from r.
func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// parseFloat parses a string as float64; ok=false for empty strings or
// non-numeric values.
func parseFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return 0, false
	}
	return float64(v), true
}
