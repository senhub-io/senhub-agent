// Package cassandra implements the free Apache Cassandra monitoring probe.
// It collects metrics via Jolokia HTTP REST, which exposes Cassandra's JMX
// MBeans over HTTP. Jolokia must be attached as a Java agent to the Cassandra
// JVM: -javaagent:/path/to/jolokia.jar=port=8778
//
// Metric naming follows OTel semantic conventions and the otelcol-contrib
// cassandraReceiver for parity where applicable.
package cassandra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in registry + YAML transformer.
const ProbeType = "cassandra"

const (
	defaultJolokiaURL = "http://localhost:8778/jolokia"
	defaultTimeout    = 10 * time.Second
	defaultInterval   = 60 * time.Second
)

// cassandraProbe collects Cassandra metrics via the Jolokia HTTP REST API.
type cassandraProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *jolokiaClient

	entityObs              entityObserver
	unregisterEntitySource func()

	// jolokiaHost and jolokiaPort are the immutable identity fields
	// extracted from cfg.JolokiaURL at construction time.
	jolokiaHost string
	jolokiaPort string
}

type probeConfig struct {
	JolokiaURL string
	Timeout    time.Duration
	Interval   time.Duration
}

// NewcassandraProbe constructs the probe. It satisfies the ProbeConstructor signature.
func NewcassandraProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.cassandra")

	cfg := probeConfig{
		JolokiaURL: defaultJolokiaURL,
		Timeout:    defaultTimeout,
		Interval:   defaultInterval,
	}
	if v, ok := config["jolokia_url"].(string); ok && v != "" {
		cfg.JolokiaURL = v
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	// Extract host and port from the Jolokia URL for entity identity.
	// Fallback to the raw URL host segment when parsing fails so the
	// probe still starts (entity reporting degrades, metrics still flow).
	jolokiaHost, jolokiaPort := parseJolokiaHostPort(cfg.JolokiaURL)

	p := &cassandraProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		jolokiaHost:  jolokiaHost,
		jolokiaPort:  jolokiaPort,
		client: &jolokiaClient{
			baseURL: cfg.JolokiaURL,
			http: &http.Client{
				Timeout: cfg.Timeout,
			},
		},
	}
	p.SetProbeType(ProbeType)
	return p, nil
}

// parseJolokiaHostPort extracts the host and port from a Jolokia URL.
// Returns ("localhost", "8778") as defaults when parsing fails.
func parseJolokiaHostPort(rawURL string) (host, port string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "localhost", "8778"
	}
	h := u.Hostname()
	p := u.Port()
	if h == "" {
		h = "localhost"
	}
	if p == "" {
		p = "8778"
	}
	return h, p
}

func (p *cassandraProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *cassandraProbe) ShouldStart() bool          { return true }
func (p *cassandraProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *cassandraProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntitySource = entity.RegisterSource(&p.entityObs)
	p.moduleLogger.Info().
		Str("jolokia_url", p.cfg.JolokiaURL).
		Msg("Starting cassandra probe")
	return nil
}

func (p *cassandraProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
	}
	p.client.http.CloseIdleConnections()
	return nil
}

// Collect gathers all Cassandra metrics from Jolokia. A connection failure
// produces up=0 and returns nil (collection error is a measurement, not a
// framework failure), consistent with the convention used by httpcheck and
// icmpcheck.
func (p *cassandraProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	up := float32(1)
	var points []data_store.DataPoint
	var cassandraVersion string

	collected, version, err := p.collect(ctx, now)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("cassandra collect failed")
		up = 0
		p.entityObs.setUp(p.jolokiaHost, p.jolokiaPort, false, "")
	} else {
		cassandraVersion = version
		points = append(points, collected...)
		p.entityObs.setUp(p.jolokiaHost, p.jolokiaPort, true, cassandraVersion)
	}

	upPoint := data_store.DataPoint{
		Name:      "senhub.cassandra.up",
		Value:     up,
		Timestamp: now,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "status"},
		},
	}
	points = append([]data_store.DataPoint{upPoint}, points...)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// collect performs all MBean reads and builds the datapoints slice.
// The second return value is the Cassandra release version (empty string when
// unavailable — the entity observer handles both cases gracefully).
func (p *cassandraProbe) collect(ctx context.Context, now time.Time) ([]data_store.DataPoint, string, error) {
	var points []data_store.DataPoint

	// Attempt to read the Cassandra release version from StorageService.
	// This is best-effort: failure does not abort collection.
	var cassandraVersion string
	if ver, err := p.client.readString(ctx,
		"org.apache.cassandra.db:type=StorageService", "ReleaseVersion"); err == nil {
		cassandraVersion = ver
	}

	// cassandra.client.connections — connectedNativeClients
	connections, err := p.client.readInt64(ctx,
		"org.apache.cassandra.metrics:type=Client,name=connectedNativeClients", "Value")
	if err != nil {
		return nil, "", fmt.Errorf("connectedNativeClients: %w", err)
	}
	points = append(points, data_store.DataPoint{
		Name:      "cassandra.client.connections",
		Value:     float32(connections),
		Timestamp: now,
		Tags:      []tags.Tag{{Key: "metric_type", Value: "connections"}},
	})

	// Per-operation metrics: Read and Write
	for _, op := range []string{"Read", "Write"} {
		opLower := toLower(op)
		mbeanLatency := fmt.Sprintf("org.apache.cassandra.metrics:type=ClientRequest,scope=%s,name=Latency", op)
		mbeanErrors := fmt.Sprintf("org.apache.cassandra.metrics:type=ClientRequest,scope=%s,name=Errors", op)

		opTags := []tags.Tag{
			{Key: "metric_type", Value: "requests"},
			{Key: "operation", Value: opLower},
		}

		// request count (Latency.Count)
		count, err := p.client.readInt64(ctx, mbeanLatency, "Count")
		if err != nil {
			return nil, "", fmt.Errorf("Latency.Count(%s): %w", op, err)
		}
		points = append(points, data_store.DataPoint{
			Name:      "cassandra.client.requests.count",
			Value:     float32(count),
			Timestamp: now,
			Tags:      opTags,
		})

		// latency mean (ms)
		mean, err := p.client.readFloat64(ctx, mbeanLatency, "Mean")
		if err != nil {
			return nil, "", fmt.Errorf("Latency.Mean(%s): %w", op, err)
		}
		points = append(points, data_store.DataPoint{
			Name:      "cassandra.client.requests.latency",
			Value:     float32(mean / 1000), // Cassandra reports in microseconds, convert to ms
			Timestamp: now,
			Tags:      opTags,
		})

		// latency p99 (ms)
		p99, err := p.client.readFloat64(ctx, mbeanLatency, "99thPercentile")
		if err != nil {
			return nil, "", fmt.Errorf("Latency.99thPercentile(%s): %w", op, err)
		}
		points = append(points, data_store.DataPoint{
			Name:      "cassandra.client.requests.latency.p99",
			Value:     float32(p99 / 1000),
			Timestamp: now,
			Tags:      opTags,
		})

		// errors count (Errors.Count)
		errCount, err := p.client.readInt64(ctx, mbeanErrors, "Count")
		if err != nil {
			return nil, "", fmt.Errorf("Errors.Count(%s): %w", op, err)
		}
		points = append(points, data_store.DataPoint{
			Name:      "cassandra.client.requests.errors",
			Value:     float32(errCount),
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "requests"},
				{Key: "operation", Value: opLower},
			},
		})
	}

	// cassandra.compaction.tasks.completed
	completed, err := p.client.readInt64(ctx,
		"org.apache.cassandra.metrics:type=Compaction,name=CompletedTasks", "Value")
	if err != nil {
		return nil, "", fmt.Errorf("CompletedTasks: %w", err)
	}
	points = append(points, data_store.DataPoint{
		Name:      "cassandra.compaction.tasks.completed",
		Value:     float32(completed),
		Timestamp: now,
		Tags:      []tags.Tag{{Key: "metric_type", Value: "compaction"}},
	})

	// cassandra.compaction.tasks.pending
	pending, err := p.client.readInt64(ctx,
		"org.apache.cassandra.metrics:type=Compaction,name=PendingTasks", "Value")
	if err != nil {
		return nil, "", fmt.Errorf("PendingTasks: %w", err)
	}
	points = append(points, data_store.DataPoint{
		Name:      "cassandra.compaction.tasks.pending",
		Value:     float32(pending),
		Timestamp: now,
		Tags:      []tags.Tag{{Key: "metric_type", Value: "compaction"}},
	})

	// cassandra.storage.load (bytes)
	load, err := p.client.readInt64(ctx,
		"org.apache.cassandra.metrics:type=Storage,name=Load", "Count")
	if err != nil {
		return nil, "", fmt.Errorf("Storage.Load: %w", err)
	}
	points = append(points, data_store.DataPoint{
		Name:      "cassandra.storage.load",
		Value:     float32(load),
		Timestamp: now,
		Tags:      []tags.Tag{{Key: "metric_type", Value: "storage"}},
	})

	// cassandra.storage.total_hints
	hints, err := p.client.readInt64(ctx,
		"org.apache.cassandra.metrics:type=Storage,name=TotalHints", "Count")
	if err != nil {
		return nil, "", fmt.Errorf("Storage.TotalHints: %w", err)
	}
	points = append(points, data_store.DataPoint{
		Name:      "cassandra.storage.total_hints",
		Value:     float32(hints),
		Timestamp: now,
		Tags:      []tags.Tag{{Key: "metric_type", Value: "storage"}},
	})

	// jvm.memory.heap.used — from java.lang:type=Memory HeapMemoryUsage
	heapMap, err := p.client.readMap(ctx, "java.lang:type=Memory", "HeapMemoryUsage")
	if err != nil {
		return nil, "", fmt.Errorf("HeapMemoryUsage: %w", err)
	}
	heapUsed, err := extractInt64FromMap(heapMap, "used")
	if err != nil {
		return nil, "", fmt.Errorf("HeapMemoryUsage.used: %w", err)
	}
	points = append(points, data_store.DataPoint{
		Name:      "jvm.memory.heap.used",
		Value:     float32(heapUsed),
		Timestamp: now,
		Tags:      []tags.Tag{{Key: "metric_type", Value: "memory"}},
	})

	// jvm.gc.collections.* — iterate over all GC collectors
	gcPoints, err := p.collectGCMetrics(ctx, now)
	if err != nil {
		return nil, "", err
	}
	points = append(points, gcPoints...)

	return points, cassandraVersion, nil
}

// collectGCMetrics reads GarbageCollector MBeans. Cassandra typically exposes
// two collectors (e.g. ParNew and ConcurrentMarkSweep, or G1 Young/Old).
func (p *cassandraProbe) collectGCMetrics(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	// The GC MBeans have dynamic names; we use the Jolokia search/list pattern
	// by reading from each known collector name. Since we can't enumerate them
	// without a list call, we use a batch read of the pattern
	// "java.lang:type=GarbageCollector,name=*" which Jolokia resolves to a map.
	raw, err := p.client.read(ctx, "java.lang:type=GarbageCollector,name=*", "CollectionCount,CollectionTime")
	if err != nil {
		return nil, fmt.Errorf("GarbageCollector: %w", err)
	}

	// Jolokia returns a map keyed by the full MBean object name when using wildcards.
	// Shape: {"java.lang:name=<GCName>,type=GarbageCollector": {"CollectionCount": N, "CollectionTime": M}}
	var outerMap map[string]map[string]json.Number
	if err := json.Unmarshal(raw, &outerMap); err != nil {
		return nil, fmt.Errorf("GarbageCollector parse: %w", err)
	}

	var points []data_store.DataPoint
	for mbeanKey, attrs := range outerMap {
		collectorName := extractGCName(mbeanKey)
		gcTags := []tags.Tag{
			{Key: "metric_type", Value: "gc"},
			{Key: "collector", Value: collectorName},
		}

		if countNum, ok := attrs["CollectionCount"]; ok {
			count, err := countNum.Int64()
			if err == nil {
				points = append(points, data_store.DataPoint{
					Name:      "jvm.gc.collections.count",
					Value:     float32(count),
					Timestamp: now,
					Tags:      gcTags,
				})
			}
		}

		if timeNum, ok := attrs["CollectionTime"]; ok {
			collTime, err := timeNum.Int64()
			if err == nil {
				points = append(points, data_store.DataPoint{
					Name:      "jvm.gc.collections.elapsed",
					Value:     float32(collTime),
					Timestamp: now,
					Tags:      gcTags,
				})
			}
		}
	}

	return points, nil
}

// extractInt64FromMap retrieves a numeric field from a map[string]interface{}
// (as returned by jolokiaClient.readMap).
func extractInt64FromMap(m map[string]interface{}, key string) (int64, error) {
	v, ok := m[key]
	if !ok {
		return 0, fmt.Errorf("key %q not found in map", key)
	}
	switch n := v.(type) {
	case json.Number:
		return n.Int64()
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	default:
		return 0, fmt.Errorf("unexpected type %T for key %q", v, key)
	}
}

// extractGCName parses the collector name from the full MBean object name.
// Example: "java.lang:name=G1 Young Generation,type=GarbageCollector" -> "G1 Young Generation"
func extractGCName(mbeanKey string) string {
	// Look for name= in the mbean key
	const prefix = "name="
	idx := 0
	for i := 0; i < len(mbeanKey)-len(prefix); i++ {
		if mbeanKey[i:i+len(prefix)] == prefix {
			idx = i + len(prefix)
			break
		}
	}
	if idx == 0 {
		return mbeanKey
	}
	name := mbeanKey[idx:]
	// Trim everything after the first comma
	for i, c := range name {
		if c == ',' {
			return name[:i]
		}
	}
	return name
}

// toLower converts an operation name like "Read" to "read".
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
