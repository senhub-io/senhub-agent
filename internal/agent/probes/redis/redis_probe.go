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
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
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
	instance     string
	moduleLogger *logger.ModuleLogger
	entityObs    *entityObserver

	unregisterEntitySource func()

	// dialFn is overridable in tests; defaults to net.DialTimeout.
	dialFn func(network, address string, timeout time.Duration) (net.Conn, error)
}

// NewRedisProbe constructs the probe from its raw params block.
func NewRedisProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	instance := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.redis")

	probe := &redisProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     instance,
		moduleLogger: moduleLogger,
		entityObs:    newEntityObserver(cfg, instance),
		dialFn:       net.DialTimeout,
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func (p *redisProbe) ShouldStart() bool          { return true }
func (p *redisProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *redisProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntitySource = entity.RegisterSource(p.entityObs)
	p.moduleLogger.Info().Str("instance", p.instance).Msg("Redis probe started")
	return nil
}

func (p *redisProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
	}
	return nil
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
		return p.BaseProbe.EnrichDataPointsWithProbeName(
			[]data_store.DataPoint{{Name: "senhub.db.up", Value: 0, Timestamp: now, Tags: p.baseTags("overview")}},
			p.GetName()), nil
	}
	defer conn.Close()

	if p.cfg.TLS {
		conn = tls.Client(conn, &tls.Config{ServerName: p.cfg.Host, MinVersion: tls.VersionTLS12})
	}

	if p.cfg.Password != "" {
		if err := sendCommand(conn, p.cfg.Timeout, "AUTH", p.cfg.Password); err != nil {
			p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis AUTH send failed")
			return p.BaseProbe.EnrichDataPointsWithProbeName(
				[]data_store.DataPoint{{Name: "senhub.db.up", Value: 0, Timestamp: now, Tags: p.baseTags("overview")}},
				p.GetName()), nil
		}
		resp, err := readLine(conn, p.cfg.Timeout)
		if err != nil || strings.HasPrefix(resp, "-") {
			if err == nil {
				err = fmt.Errorf("auth rejected: %s", resp)
			}
			p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis AUTH failed")
			return p.BaseProbe.EnrichDataPointsWithProbeName(
				[]data_store.DataPoint{{Name: "senhub.db.up", Value: 0, Timestamp: now, Tags: p.baseTags("overview")}},
				p.GetName()), nil
		}
	}

	if err := sendCommand(conn, p.cfg.Timeout, "INFO", "all"); err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO send failed")
		return p.BaseProbe.EnrichDataPointsWithProbeName(
			[]data_store.DataPoint{{Name: "senhub.db.up", Value: 0, Timestamp: now, Tags: p.baseTags("overview")}},
			p.GetName()), nil
	}

	blob, err := readInfo(conn, p.cfg.Timeout)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("Redis INFO read failed")
		return p.BaseProbe.EnrichDataPointsWithProbeName(
			[]data_store.DataPoint{{Name: "senhub.db.up", Value: 0, Timestamp: now, Tags: p.baseTags("overview")}},
			p.GetName()), nil
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

	// INFO sentinel — only when redis_mode=sentinel in INFO server section.
	var sentinelInfo map[string]string
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
	}

	points := p.buildDatapoints(info, cmdStats, clusterInfo, sentinelInfo, now, 1)
	p.entityObs.update(p.cfg, info)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// baseTags returns the common tags emitted on every datapoint.
func (p *redisProbe) baseTags(metricType string) []tags.Tag {
	return []tags.Tag{
		{Key: "instance", Value: p.instance},
		{Key: "db.system.name", Value: "redis"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
		{Key: "metric_type", Value: metricType},
	}
}

func (p *redisProbe) addGauge(out *[]data_store.DataPoint, name string, value float32, ts time.Time, metricType string, extra ...tags.Tag) {
	t := p.baseTags(metricType)
	t = append(t, extra...)
	*out = append(*out, data_store.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: t})
}

func (p *redisProbe) addCounter(out *[]data_store.DataPoint, name string, value float32, ts time.Time, metricType string, extra ...tags.Tag) {
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
func (p *redisProbe) buildDatapoints(info map[string]string, cmdStats map[string]cmdStat, clusterInfo map[string]string, sentinelInfo map[string]string, ts time.Time, up float32) []data_store.DataPoint {
	var pts []data_store.DataPoint

	p.addGauge(&pts, "senhub.db.up", up, ts, "overview")
	if up == 0 {
		return pts
	}

	// overview
	if v, ok := parseFloat(info["uptime_in_seconds"]); ok {
		p.addCounter(&pts, "redis.uptime", v, ts, "overview")
	}
	if ver := info["redis_version"]; ver != "" {
		p.addGauge(&pts, "senhub.db.version.info", 1, ts, "overview",
			tags.Tag{Key: "version", Value: ver})
	}

	// connections
	if v, ok := parseFloat(info["connected_clients"]); ok {
		p.addGauge(&pts, "redis.clients.connected", v, ts, "connections")
	}
	if v, ok := parseFloat(info["blocked_clients"]); ok {
		p.addGauge(&pts, "redis.clients.blocked", v, ts, "connections")
	}
	if v, ok := parseFloat(info["total_connections_received"]); ok {
		p.addCounter(&pts, "redis.connections.received", v, ts, "connections")
	}
	if v, ok := parseFloat(info["rejected_connections"]); ok {
		p.addCounter(&pts, "redis.connections.rejected", v, ts, "connections")
	}

	// memory
	if v, ok := parseFloat(info["used_memory"]); ok {
		p.addGauge(&pts, "redis.memory.used", v, ts, "memory")
	}
	if v, ok := parseFloat(info["used_memory_rss"]); ok {
		p.addGauge(&pts, "redis.memory.used.rss", v, ts, "memory")
	}
	if v, ok := parseFloat(info["used_memory_peak"]); ok {
		p.addGauge(&pts, "redis.memory.peak", v, ts, "memory")
	}
	if v, ok := parseFloat(info["mem_fragmentation_ratio"]); ok {
		p.addGauge(&pts, "redis.memory.fragmentation.ratio", v, ts, "memory")
	}

	// throughput
	if v, ok := parseFloat(info["total_commands_processed"]); ok {
		p.addCounter(&pts, "redis.commands.processed", v, ts, "throughput")
	}
	if v, ok := parseFloat(info["total_net_input_bytes"]); ok {
		p.addCounter(&pts, "redis.net.input", v, ts, "throughput")
	}
	if v, ok := parseFloat(info["total_net_output_bytes"]); ok {
		p.addCounter(&pts, "redis.net.output", v, ts, "throughput")
	}
	if v, ok := parseFloat(info["instantaneous_ops_per_sec"]); ok {
		p.addGauge(&pts, "redis.ops.per_sec", v, ts, "throughput")
	}

	// cache / keyspace hits
	hits, hitsOK := parseFloat(info["keyspace_hits"])
	misses, missesOK := parseFloat(info["keyspace_misses"])
	if hitsOK {
		p.addCounter(&pts, "redis.keyspace.hits", hits, ts, "cache")
	}
	if missesOK {
		p.addCounter(&pts, "redis.keyspace.misses", misses, ts, "cache")
	}
	sum := hits + misses
	ratio := float32(0)
	if sum > 0 {
		ratio = hits / sum
	}
	p.addGauge(&pts, "redis.keyspace.hit.ratio", ratio, ts, "cache")

	// keyspace per-db (lines like db0:keys=N,expires=M,avg_ttl=T)
	for key, val := range info {
		if !strings.HasPrefix(key, "db") {
			continue
		}
		dbIdx, keys, expires, avgTTL, ok := parseKeyspaceLine(key + ":" + val)
		if !ok {
			continue
		}
		dbTag := tags.Tag{Key: "db", Value: dbIdx}
		p.addGauge(&pts, "redis.db.keys", float32(keys), ts, "keyspace", dbTag)
		p.addGauge(&pts, "redis.db.expires", float32(expires), ts, "keyspace", dbTag)
		p.addGauge(&pts, "redis.db.avg_ttl", float32(avgTTL), ts, "keyspace", dbTag)
	}

	// replication
	role := info["role"]
	roleVal := float32(-1)
	switch role {
	case "master":
		roleVal = 1
	case "slave", "replica":
		roleVal = 0
	}
	p.addGauge(&pts, "redis.replication.role", roleVal, ts, "replication")

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
		p.addCounter(&pts, "redis.replication.offset", v, ts, "replication")
	}

	if role == "master" {
		if v, ok := parseFloat(info["connected_slaves"]); ok {
			p.addGauge(&pts, "redis.replication.slaves.connected", v, ts, "replication")
		}
	} else {
		if v, ok := parseFloat(info["master_last_io_seconds_ago"]); ok {
			p.addGauge(&pts, "redis.replication.lag", v, ts, "replication")
		}
	}

	// persistence
	if v, ok := parseFloat(info["rdb_changes_since_last_save"]); ok {
		p.addGauge(&pts, "redis.rdb.changes", v, ts, "persistence")
	}
	if v, ok := parseFloat(info["aof_enabled"]); ok {
		p.addGauge(&pts, "redis.aof.enabled", v, ts, "persistence")
	}
	if v, ok := parseFloat(info["rdb_last_bgsave_time_sec"]); ok {
		p.addGauge(&pts, "redis.rdb.last_bgsave.duration", v, ts, "persistence")
	}
	if lastSaveStr := info["rdb_last_save_time"]; lastSaveStr != "" {
		if lastSaveEpoch, err := strconv.ParseInt(strings.TrimSpace(lastSaveStr), 10, 64); err == nil && lastSaveEpoch > 0 {
			age := ts.Unix() - lastSaveEpoch
			if age < 0 {
				age = 0
			}
			p.addGauge(&pts, "redis.rdb.last_save.age", float32(age), ts, "persistence")
		}
	}

	// cpu
	for _, state := range []struct{ field, label string }{
		{"used_cpu_sys", "sys"},
		{"used_cpu_user", "user"},
		{"used_cpu_sys_children", "sys_children"},
		{"used_cpu_user_children", "user_children"},
	} {
		if v, ok := parseFloat(info[state.field]); ok {
			p.addCounter(&pts, "redis.cpu.time", v, ts, "cpu",
				tags.Tag{Key: "state", Value: state.label})
		}
	}

	// memory — additional fields
	if v, ok := parseFloat(info["used_memory_lua"]); ok {
		p.addGauge(&pts, "redis.memory.lua", v, ts, "memory")
	}

	// client buffers
	if v, ok := parseFloat(info["client_recent_max_input_buffer"]); ok {
		p.addGauge(&pts, "redis.clients.max_input_buffer", v, ts, "connections")
	}
	if v, ok := parseFloat(info["client_recent_max_output_buffer"]); ok {
		p.addGauge(&pts, "redis.clients.max_output_buffer", v, ts, "connections")
	}

	// fork duration
	if v, ok := parseFloat(info["latest_fork_usec"]); ok {
		p.addGauge(&pts, "redis.latest.fork", v, ts, "persistence")
	}

	// replication backlog
	if v, ok := parseFloat(info["repl_backlog_first_byte_offset"]); ok {
		p.addGauge(&pts, "redis.replication.backlog_first_byte_offset", v, ts, "replication")
	}

	// evicted and expired keys
	if v, ok := parseFloat(info["evicted_keys"]); ok {
		p.addCounter(&pts, "redis.evicted_keys", v, ts, "cache")
	}
	if v, ok := parseFloat(info["expired_keys"]); ok {
		p.addCounter(&pts, "redis.expired_keys", v, ts, "cache")
	}

	// per-command stats
	for cmd, cs := range cmdStats {
		cmdTag := tags.Tag{Key: "cmd", Value: cmd}
		p.addCounter(&pts, "redis.cmd.calls", float32(cs.calls), ts, "commands", cmdTag)
		p.addCounter(&pts, "redis.cmd.usec", float32(cs.usec), ts, "commands", cmdTag)
	}

	// cluster metrics (INFO cluster — only populated when cluster_enabled=1)
	if clusterInfo != nil {
		// cluster_state: "ok" → 1, anything else → 0.
		stateVal := float32(0)
		if clusterInfo["cluster_state"] == "ok" {
			stateVal = 1
		}
		p.addGauge(&pts, "redis.cluster.state", stateVal, ts, "cluster")

		if v, ok := parseFloat(clusterInfo["cluster_slots_assigned"]); ok {
			p.addGauge(&pts, "redis.cluster.slots.assigned", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_slots_ok"]); ok {
			p.addGauge(&pts, "redis.cluster.slots.ok", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_slots_pfail"]); ok {
			p.addGauge(&pts, "redis.cluster.slots.pfail", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_slots_fail"]); ok {
			p.addGauge(&pts, "redis.cluster.slots.fail", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_stats_messages_sent"]); ok {
			p.addCounter(&pts, "redis.cluster.links.created", v, ts, "cluster")
		}
		if v, ok := parseFloat(clusterInfo["cluster_stats_messages_received"]); ok {
			p.addCounter(&pts, "redis.cluster.links.disconnected", v, ts, "cluster")
		}
	}

	// sentinel metrics (INFO sentinel — only populated in sentinel mode)
	if sentinelInfo != nil {
		if v, ok := parseFloat(sentinelInfo["sentinel_masters"]); ok {
			p.addGauge(&pts, "redis.sentinel.masters", v, ts, "sentinel")
		}
		if v, ok := parseFloat(sentinelInfo["sentinel_running_scripts"]); ok {
			p.addGauge(&pts, "redis.sentinel.scripts_queue_length", v, ts, "sentinel")
		}
		// Aggregate ok/total slaves and sentinels across all monitored masters.
		// Each master is reported as: sentinel_mastersN:name=...,status=...,slaves=N,sentinels=M
		// Parallel fields sentinel_slavesN_... and sentinel_sentinelsN_... provide per-master detail.
		totalSlaves, okSlaves, totalSentinels, okSentinels := parseSentinelMasterStats(sentinelInfo)
		p.addGauge(&pts, "redis.sentinel.slaves", float32(totalSlaves), ts, "sentinel")
		p.addGauge(&pts, "redis.sentinel.ok_slaves", float32(okSlaves), ts, "sentinel")
		p.addGauge(&pts, "redis.sentinel.sentinels", float32(totalSentinels), ts, "sentinel")
		p.addGauge(&pts, "redis.sentinel.ok_sentinels", float32(okSentinels), ts, "sentinel")
	}

	// tracking metrics (RESP3 client-side tracking, present in INFO clients
	// only when at least one client uses TRACKING; parse defensively).
	if v, ok := parseFloat(info["tracking_clients"]); ok {
		p.addGauge(&pts, "redis.tracking.clients", v, ts, "tracking")
	}
	if v, ok := parseFloat(info["tracking_table_used_keys"]); ok {
		p.addGauge(&pts, "redis.tracking.keys", v, ts, "tracking")
	}

	return pts
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
func parseKeyspaceLine(line string) (dbIdx string, keys int64, expires int64, avgTTL int64, ok bool) {
	// line = "db0:keys=100,expires=5,avg_ttl=3000"
	dbPart, rest, cut := strings.Cut(line, ":")
	if !cut {
		return "", 0, 0, 0, false
	}
	if !strings.HasPrefix(dbPart, "db") {
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

// parseFloat parses a string as float32; ok=false for empty strings or
// non-numeric values.
func parseFloat(s string) (float32, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return 0, false
	}
	return float32(v), true
}
