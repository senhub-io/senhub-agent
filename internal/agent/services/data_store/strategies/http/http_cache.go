// senhub-agent/internal/agent/services/data_store/http_cache.go
package http

import (
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// DefaultMaxCacheSeries caps the MetricCache cardinality, mirroring the
// OTLP strategy's DefaultMaxStoreSize. The cache is fed by every probe
// including otlp_receiver and prometheus_scrape, whose series sets are
// controlled by EXTERNAL senders — without a cap a remote producer can
// inflate the cache without bound (memory ceiling asymmetry with the
// capped OTLP store, audit finding P2 / #281). Operators expecting more
// distinct series raise `max_cache_size` on the http strategy params;
// 0 means unbounded.
const DefaultMaxCacheSeries = 50000

// dropReasonCacheCap labels datapoints refused because the cache holds
// maxSeries distinct series. Surfaces as the `reason` attribute on the
// `senhub.agent.cache.dropped` counter.
const dropReasonCacheCap = "http_cache_cap"

// DiscriminantTagsRegistry defines which tags are discriminant (identify unique instances)
// vs contextual (provide metadata) for each probe type.
//
// Discriminant tags MUST be included in the time series key because metrics with different
// discriminant tag values represent DIFFERENT physical/logical instances that can have
// different metric values at the same timestamp.
//
// Contextual tags should NOT be included in the key - they provide additional metadata
// but don't identify distinct metric sources. Including them would break time series
// continuity when metadata changes (e.g., endpoint URL change, platform info update).
//
// DiscriminantTagsRegistry defines which tags are discriminant (identify unique instances)
// vs contextual (provide metadata) for each probe type.
//
// Based on TIME_SERIES_KEY_DESIGN.md - Universal Uniqueness Rule (RUU):
// "Une clé de série temporelle DOIT être unique SI ET SEULEMENT SI
// les valeurs des métriques collectées à cet instant peuvent être DIFFÉRENTES"
//
// See docs/engineering/TIME_SERIES_KEY_DESIGN.md for:
// - Complete design rationale and test scenarios
// - Rules for classifying tags as discriminant vs contextual
// - Step-by-step guide for adding new probe types
// - Troubleshooting cache key issues
var DiscriminantTagsRegistry = map[string][]string{
	// Application server probes
	"apache": {"state", "metric_type"}, // apache.workers{state=busy|idle}

	// System probes - multi-instance metrics
	"cpu":         {"core"},                           // Different CPU cores have independent values
	"memory":      {},                                 // System-level only, no instances
	// process: one series per running process identified by pid+name;
	// aggregate (process.count) is discriminated by name only.
	"process": {"process.pid", "process.name"},
	"network":     {"interface", "adapter"},           // Different network interfaces
	"logicaldisk": {"drive", "mount_point", "device"}, // Different drives/volumes

	// Application probes
	"phpfpm":  {"pool"},                            // One series per PHP-FPM pool
	"citrix":  {"metric_type", "failure_category"}, // Citrix aggregation types
	"webapp":  {"url", "endpoint"},                 // Different web endpoints
	"gateway": {"destination", "target"},           // Different gateway targets
	"envoy":   {"cluster"},                         // Per-cluster upstream metrics (envoy.cluster.*)
	"netscaler": {
		"vserver", "service", "servicegroup", // Load Balancing
		"interface",   // Network interfaces
		"partition",   // Disk partitions
		"certname",    // SSL certificates
		"csvserver",   // Content Switching vServers
		"cspolicy",    // Content Switching policies
		"gslbvserver", // GSLB vServers
		"gslbsite",    // GSLB sites
		"gslbservice", // GSLB services
		"authvserver", // AAA authentication vServers
		"vpnvserver",  // VPN vServers
		"ha_node_id",  // High Availability nodes (ID and IP both discriminant)
		"ha_node_ip",  // HA node IP address (for ShowTags=false support)
	},

	// Hardware sensor probes — one series per sensor instance (hardware.component
	// carries the sensor name: "CPU Temp", "FAN1", "12V", …).
	"ipmi": {"hardware.component"},

	// Infrastructure probes
	"redfish": {
		// Storage components
		"controller", "controller_id",
		"drive_id", "drive_name",
		"volume_id", "volume_name",
		"pool_name", "pool_id",
		// Other hardware components
		"psu_name", "psu_id",
		"processor_id",
		"memory_module_id",
		"fan_name", "sensor_name",
	},

	// Backup probes
	"veeam": {
		"metric_type",          // Category filtering
		"job_name", "job_type", // Backup jobs
		"repo_name",                  // Repositories
		"proxy_name",                 // Proxies
		"object_name", "object_type", // Protected objects
		"server_name", "server_type", // Managed servers
	},

	// Event probes
	"winevents": {"event_id", "source"}, // Windows Event Log events
	"syslog":    {"event_id", "source"}, // Syslog events
	// systemd: one series per supervised unit; systemd.unit is the sole
	// discriminant declared in multi_instance_labels.
	"systemd": {"systemd.unit"},

	// Application / middleware probes
	"wildfly": {"datasource"}, // Per-datasource JDBC pool metrics (wildfly.datasource.connections.*)
	// Observability / messaging probes — one series per broker endpoint
	"pulsar": {"endpoint"}, // Apache Pulsar: one broker per endpoint URL
	// Storage probes — one series per physical device.
	"smart": {"smart.device"}, // S.M.A.R.T.: one series per disk (sata/nvme)

	// SNMP polling — one series per (target, interface row); metric_type
	// separates interface / system / status families.
	// consul: health.checks emits one series per state (critical/warning/passing).
	"consul":      {"metric_type", "state"},
	"dns_latency": {"name", "resolver", "metric_type"},
	"docker":      {"container_id", "container_name", "metric_type", "core"},
	"http_check":  {"target", "metric_type"},
	"icmp_check":  {"target", "metric_type"},
	"tcp_dial":    {"target", "metric_type"},
	"snmp_poll":   {"instance", "if_index", "metric_type"},
	// haproxy: one series per (proxy, component) pair; metric_type
	// separates sessions / throughput / error / request families.
	"haproxy": {"proxy", "component", "metric_type"},
	// elasticsearch: GC collectors, indexing/search operations, and thread
	// pools each emit multiple datapoints under the same metric name.
	"elasticsearch": {
		"metric_type",
		"collector",    // elasticsearch.jvm.gc.collections.* — young|old
		"operation",    // elasticsearch.indexing/search.operations.* — index|query|fetch
		"thread_pool",  // elasticsearch.thread_pool.tasks.* — per thread pool name
	},
	// hyperv: per-VM series are discriminated by vm name; vm.count by state bucket.
	"hyperv": {"hyperv.vm.name", "state", "metric_type"},
	// modbus: register.name and register.address identify the register;
	// host and modbus.unit_id distinguish probe instances.
	"modbus": {"register.name", "register.address", "host", "modbus.unit_id", "metric_type"},
	// prometheus_scrape: scraped label sets are arbitrary and cannot be
	// enumerated here; per-target series stay distinct, finer label
	// splits collapse on the cache-keyed sinks (same limitation as
	// otlp_receiver). The OTLP/Prometheus re-export path carries all
	// labels through the mapper pass-through.
	"prometheus_scrape": {"target", "metric_type"},
	// exec: dynamic perfdata/JSON metric names carry identity in the
	// metric name itself (senhub.exec.<label>); no per-series labels to
	// discriminate beyond the probe instance.
	"exec": {"metric_type"},
	// varnish: cache.operations collapsed via result tag; thread.operations
	// via operation tag; all other metrics are single-instance per probe.
	"varnish": {"result", "operation", "metric_type"},

	// Messaging probes
	"kafka": {
		"topic",        // per-topic metrics (kafka.topic.partitions, lag_sum, …)
		"partition",    // per-partition metrics (offsets, replicas, consumer offsets/lag)
		"group",        // per-consumer-group metrics (members, offset, lag, lag_sum)
		"metric_type",  // separates broker / topic / partition / consumer_group families
	},
	// jenkins: job/node/executor counts collapse onto one metric name per
	// family, discriminated by status (success/failure/…), state (busy/free)
	// or job name; metric_type separates the jobs/nodes/queue families.
	"jenkins": {"job", "status", "state", "metric_type"},

	// Cassandra — operation (read|write) discriminates request metrics;
	// collector discriminates GC metrics. metric_type separates families.
	"cassandra": {"operation", "collector", "metric_type"},
	// opensearch: GC collectors, indexing/search operations, and thread
	// pools are the three axes that produce distinct per-series values.
	"opensearch": {
		"collector",    // opensearch.jvm.gc.collections.* — young|old
		"operation",    // opensearch.indexing/search.operations.* — index|query|fetch
		"thread_pool",  // opensearch.thread_pool.tasks.* — per thread pool name
	},

	// Application monitoring probes
	"solr": {"core"}, // per-core metrics (solr.document.count, solr.index.size)
	// memcached: network by direction (transmit/receive), operations by result
	// (hit/miss), commands by command (get/set/flush), cpu.usage by state (user/system).
	"memcached": {"result", "command", "state", "direction", "metric_type"},
	// nvidia: one series per GPU card; gpu.index + gpu.name uniquely
	// identify a card within the host, gpu.uuid is added for stable joins.
	"nvidia": {"gpu.index", "gpu.name", "gpu.uuid", "metric_type"},
	// unifi: per-type inventory (device_type), per-device health
	// (device_name+device_type), per-AP stats (device_name), WAN
	// throughput (direction=transmit|receive via network.io.direction) —
	// all distinct time series that can coexist in a single cycle.
	"unifi": {"device_type", "device_name", "direction"},
	// winservices: per-service metrics (windows.service.state /
	// windows.service.status) are discriminated by service name; without
	// this tag in the key all services collapse to one cache slot.
	"winservices": {"windows.service.name", "metric_type"},

	// Database probes — the probes emit multiple datapoints per OTel metric
	// name discriminated by attribute tags (see docs/developer-guide/otel/
	// senhub-semantic-conventions.md §4.13 for the full collapse list).
	"clickhouse": {"instance"}, // multi-instance: one series per scraped endpoint
	"redis": {
		"instance",    // one probe per Redis server (host:port)
		"db",          // redis.db.keys{db=0|1|...} / redis.db.expires / redis.db.avg_ttl — per-logical-db
		"state",       // redis.cpu.time{state=sys|user|sys_children|user_children}
		"cmd",         // redis.cmd.calls{cmd=get|set|...} / redis.cmd.usec — per-command
		"metric_type", // separates overview / connections / memory / throughput / cache / keyspace / replication / persistence / cpu / commands families
	"mssql": {
		"database",  // sqlserver.database.io{database=…} + sqlserver.database.status{database=…}
		"direction", // sqlserver.database.io{direction=read|write}
	},
	"mysql": {
		"kind",         // mysql.threads{kind=running|connected}
		"command",      // mysql.commands{command=select|insert|...}
		"error",        // mysql.connection.errors{error=aborted_clients|...}
		"status",       // mysql.buffer_pool.data_pages{status=dirty}
		"state",        // senhub.db.mysql.transaction.count{state=committed|rolled_back}
		"io.direction", // senhub.db.mysql.io{io.direction=read|write}
		"role",         // senhub.db.replication.role/health (per-instance, role tag carries semantic)
		"database",     // per-database opt-in metrics
		"table",        // per-table opt-in metrics
	},
	"postgresql": {
		"state",     // postgresql.backends{state=active|idle|idle_in_transaction}
		"operation", // postgresql.wal.lag{operation=replay}
		"role",      // senhub.db.replication.role/health
		"schema",    // bloat per-table
		"table",     // bloat per-table + size per-table
		"database",  // per-database opt-in metrics
	},
	"oracle": {
		"instance",   // one series per monitored db (oracle://host:port/service)
		"status",     // oracle.sessions.count{status=active|inactive}
		"tablespace", // oracle.tablespace.used/total{tablespace=...}
		"wait_class", // oracle.wait_class.total{wait_class=...}
		"metric_type",
	},

	// Message broker probes
	"rabbitmq": {"node", "vhost", "queue"}, // per-node (node) and per-queue (vhost+queue) metrics
	// ActiveMQ — per-destination metrics use destination + destination_type
	// to distinguish queues from topics and individual destination instances.
	"activemq": {"destination", "destination_type", "metric_type"},
	// Ceph — cluster-level metrics are single-instance on "instance"
	// (the REST API endpoint); pool metrics add "pool" to disambiguate
	// per-pool series (ceph.pool.*).
	"ceph": {"instance", "pool"},
	// Proxmox VE — one series per node, per VM/container (vmid), and per
	// storage pool. proxmox.vm.type (qemu/lxc) is contextual: two VMs with
	// the same vmid but different types cannot coexist, so it does not add
	// discriminating power and is intentionally omitted.
	"proxmox": {
		"proxmox.node",    // Proxmox cluster node name
		"proxmox.vmid",    // VM/LXC numeric ID (unique per cluster)
		"proxmox.vm.name", // VM/LXC display name (redundant with vmid but declared in multi_instance_labels)
		"proxmox.storage", // Storage pool name
	},

	// Kubernetes — one series per resource instance; the discriminant
	// tag identifies the node, pod, container, or deployment.
	"kubernetes": {
		"k8s.node.name",       // per-node metrics (k8s.node.*)
		"k8s.pod.name",        // per-pod metrics (k8s.pod.*)
		"k8s.namespace.name",  // namespace scopes pods, containers, deployments
		"k8s.container.name",  // per-container metrics (k8s.container.*)
		"k8s.deployment.name", // per-deployment metrics (k8s.deployment.*)
	},

	// Application server probes
	"tomcat": {
		"connector", // HTTP/AJP connector (requests, bytes, threads, errors, processing_time)
		"collector", // JVM GC collector (gc count + elapsed)
		"context",   // Servlet context (sessions)
	},

	// IBM i / Power Systems — collectors emit multiple rows per metric
	// name, one per resource instance. These tags identify the instance.
	"ibmi": {
		// Job identity (active_job, msgw_job, scheduled_job top-N,
		// query_supervisor). job_name is fully-qualified NUMBER/USER/PROG.
		"job_name", "job_user", "subsystem", "internal_job_id",
		// Work queues & spool backlog (job_queue, output_queue,
		// message_queue multi-instance, spooled_file).
		"queue_name", "queue_library",
		// Storage pools & ASPs (asp, memory_pool, disk_status).
		"asp_number", "pool_name", "pool_id",
		"unit_number", "device_name",
		// DB & journaling (sys_table_stats, index_advisor, journal_*).
		"table_schema", "table_name", "key_columns",
		"journal", "journal_library",
		"receiver", "receiver_library",
		// Identity, security, config (user_profile, user_storage,
		// system_value, library_list, license).
		"user", "user_name", "schema",
		"sysval",
		"library",
		"product_id", "feature_id",
		// Network (netstat_listener, netstat_interface,
		// netstat_connection, http_server, jvm).
		"address", "local_port", "port_name", "protocol",
		"tcp_state", "interface",
		"server_name",
		// Compliance & hardware (ptf_group, watch_info,
		// hardware_resource, authority_collection).
		"group", "session_id", "category",
		// Dimensions on count_by_* aggregates.
		"status", "state", "job_type", "type",
		// Event discriminants (history_log, message_queue,
		// audit_journal, msgw_job events).
		"message_id", "message_type", "entry_type",
		"from_job", "from_user", "from_program",
		// Probe self-observability (per-collector health metrics).
		"collector",
	},

	// CouchDB — method and status collapse one OTel name onto N series.
	"couchdb": {"method", "status"},
}

// MetricCache stores the latest metrics in memory with TTL, organized like a TSDB
type MetricCache struct {
	mu sync.RWMutex
	// TSDB-like structure: unique key -> latest metric
	// Key format: probe_name:metric_name:discriminant_tags
	// Example: "cpu:usage_percent:core=0" or "redfish:storage.drive.temperature:drive_id=disk.bay.0"
	// Only discriminant tags are in the key - contextual tags are in CachedMetric.Tags
	timeSeries map[string]CachedMetric
	// Index by probe for fast probe-specific queries
	probeIndex map[string]map[string]bool // probe_name -> set of ts_keys
	ttl        time.Duration
	// maxSeries caps the number of distinct time series. Once reached,
	// NEW series are dropped (counted under reason "http_cache_cap")
	// while existing series keep updating — continuity of known series
	// is preferred over admitting unbounded new cardinality. TTL
	// eviction frees slots, so a dropped-then-expired series can be
	// re-admitted later. 0 = unbounded.
	maxSeries int
	stopChan  chan struct{}
	logger    *logger.ModuleLogger
}

// CachedMetric represents a stored metric with metadata
type CachedMetric struct {
	Value      interface{}
	Timestamp  time.Time
	Unit       string
	ProbeName  string
	MetricName string
	Tags       map[string]string
}

// NewMetricCache creates a new metric cache with the specified TTL and
// the default cardinality cap (override via SetMaxSeries).
func NewMetricCache(ttl time.Duration, logger *logger.ModuleLogger) *MetricCache {
	return &MetricCache{
		timeSeries: make(map[string]CachedMetric),
		probeIndex: make(map[string]map[string]bool),
		ttl:        ttl,
		maxSeries:  DefaultMaxCacheSeries,
		logger:     logger,
	}
}

// SetMaxSeries overrides the cardinality cap. 0 disables it. Existing
// entries above a lowered cap are not evicted — the cap only governs
// admission of new series.
func (c *MetricCache) SetMaxSeries(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxSeries = n
}

// generateTimeSeriesKey creates a unique key for a time series based on probe, metric name,
// and ONLY discriminant tags (tags that identify unique metric instances).
//
// This implements the Universal Uniqueness Rule (RUU) from TIME_SERIES_KEY_DESIGN.md:
// Keys are unique IF AND ONLY IF the metric values can be different.
//
// Example:
//   - CPU probe with core=0 and core=1 → different keys (different CPU cores)
//   - Same CPU core with different "platform" tags → SAME key (contextual metadata)
//   - Redfish with different drive_id → different keys (different physical drives)
//   - Same drive with different "endpoint" URL → SAME key (same physical drive, DNS changed)
//
// This ensures:
//   - Time series continuity when metadata changes (endpoint, hostname, etc.)
//   - Proper cardinality (only multi-instance metrics create multiple series)
//   - Filtering still works (all tags preserved in CachedMetric.Tags)
func (c *MetricCache) generateTimeSeriesKey(probeName, probeType, metricName string, tags map[string]string) string {
	// Get discriminant tags for this probe type from registry
	// Use probeType (technical identifier: "redfish", "cpu", etc.) for lookup
	// NOT probeName (unique instance name: "redfish", "redfish2", etc.)
	discriminantTagNames, exists := DiscriminantTagsRegistry[probeType]
	if !exists {
		// Unknown probe type - log warning and use no discriminant tags
		// This is safe: creates single time series per metric (like system-level probes)
		c.logger.Warn().
			Str("probe_name", probeName).
			Str("probe_type", probeType).
			Str("metric_name", metricName).
			Msg("⚠️ Probe type not in DiscriminantTagsRegistry - using no discriminant tags")
		discriminantTagNames = []string{}
	}

	// Extract only discriminant tag values that are present
	var tagParts []string
	discriminantTagKeys := make([]string, 0, len(discriminantTagNames))

	for _, tagName := range discriminantTagNames {
		if _, exists := tags[tagName]; exists {
			discriminantTagKeys = append(discriminantTagKeys, tagName)
		}
	}

	// Sort discriminant tag keys for consistent key generation
	for i := 0; i < len(discriminantTagKeys); i++ {
		for j := i + 1; j < len(discriminantTagKeys); j++ {
			if discriminantTagKeys[i] > discriminantTagKeys[j] {
				discriminantTagKeys[i], discriminantTagKeys[j] = discriminantTagKeys[j], discriminantTagKeys[i]
			}
		}
	}

	// Build tag string from discriminant tags only
	for _, k := range discriminantTagKeys {
		tagParts = append(tagParts, fmt.Sprintf("%s=%s", k, tags[k]))
	}

	// Create unique key: probe:metric:discriminant_tags
	if len(tagParts) > 0 {
		return fmt.Sprintf("%s:%s:%s", probeName, metricName, joinStrings(tagParts, ","))
	}
	return fmt.Sprintf("%s:%s", probeName, metricName)
}

// AddDataPointsWithTransformer adds data points to the cache using external transformer
func (c *MetricCache) AddDataPointsWithTransformer(dataPoints []datapoint.DataPoint, transformerRegistry *transformers.TransformerRegistry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Debug().
		Int("data_points", len(dataPoints)).
		Msg("💾 Cache - Adding data points")

	now := time.Now()

	for _, dp := range dataPoints {
		// Convert tags from []tags.Tag to map[string]string
		tags := make(map[string]string)
		for _, tag := range dp.Tags {
			tags[tag.Key] = tag.Value
		}

		// Get probe name and type from tags
		probeName := tags["probe_name"]
		probeType := tags["probe_type"]

		// ⚠️ DEBUG: Log if probe_name or probe_type is missing or empty
		if probeName == "" {
			c.logger.Warn().
				Str("metric_name", dp.Name).
				Interface("all_tags", tags).
				Msg("⚠️ MISSING PROBE_NAME: Metric has no probe_name tag!")
			probeName = "unknown" // Fallback for metrics without probe_name
		}
		if probeType == "" {
			c.logger.Error().
				Str("metric_name", dp.Name).
				Str("probe_name", probeName).
				Interface("all_tags", tags).
				Msg("⚠️ MISSING PROBE_TYPE: Metric has no probe_type tag! Probe not properly initialized with SetProbeType(). Falling back to probe_name.")
			probeType = probeName // Fallback to probe_name if type missing
		}

		// Generate unique time series key
		tsKey := c.generateTimeSeriesKey(probeName, probeType, dp.Name, tags)

		// Get transformer to resolve unit
		// IMPORTANT: Use probeType (technical identifier like "redfish", "cpu")
		// NOT probeName (display name like "storage-me5024", "redfish2")
		// This ensures multiple probes of the same type share the same transformer definitions
		transformer, err := transformerRegistry.LoadTransformer(probeType, "friendly")
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("probe_name", probeName).
				Str("probe_type", probeType).
				Msg("Failed to get transformer for unit resolution")
		}

		// Resolve unit using transformer
		unit := ""
		if transformer != nil {
			unit = transformer.GetUnit(dp.Name)
		}

		// Note: Unit corrections are now applied earlier in the data processing pipeline (data_store.go)
		// before routing to strategies, so datapoints here already have corrected values

		// Store the metric (value already corrected by data_store.go)
		cachedMetric := CachedMetric{
			Value:      dp.Value,
			Timestamp:  now, // Use consistent timestamp for write batch
			Unit:       unit,
			ProbeName:  probeName,
			MetricName: dp.Name,
			Tags:       tags,
		}

		// TSDB approach: Store/replace metric by unique key (deduplication at write-time)
		existingMetric, exists := c.timeSeries[tsKey]
		if exists {
			c.logger.Debug().
				Str("ts_key", tsKey).
				Time("old_timestamp", existingMetric.Timestamp).
				Time("new_timestamp", cachedMetric.Timestamp).
				Msg("🔄 Replacing existing metric in time series")
		} else {
			// Cardinality cap: refuse NEW series past maxSeries while
			// existing series keep updating. TTL cleanup frees slots.
			if c.maxSeries > 0 && len(c.timeSeries) >= c.maxSeries {
				agentstate.IncrementHTTPCacheDropped(dropReasonCacheCap)
				c.logger.Debug().
					Str("ts_key", tsKey).
					Str("metric_name", dp.Name).
					Str("probe_name", probeName).
					Int("max_series", c.maxSeries).
					Msg("Cache at cardinality cap - dropping new time series")
				continue
			}
			c.logger.Debug().
				Str("ts_key", tsKey).
				Str("metric_name", dp.Name).
				Str("probe_name", probeName).
				Msg("📊 Adding new metric to time series")
		}

		c.timeSeries[tsKey] = cachedMetric

		// Update probe index
		if c.probeIndex[probeName] == nil {
			c.probeIndex[probeName] = make(map[string]bool)
		}
		c.probeIndex[probeName][tsKey] = true

		c.logger.Debug().
			Str("ts_key", tsKey).
			Str("probe", probeName).
			Str("metric", dp.Name).
			Interface("value", dp.Value).
			Str("unit", unit).
			Msg("💾 Cache - Stored metric")
	}
}

// cleanup removes expired metrics from the cache
func (c *MetricCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	expiredKeys := make([]string, 0)

	// Find expired metrics
	for key, metric := range c.timeSeries {
		if now.Sub(metric.Timestamp) > c.ttl {
			expiredKeys = append(expiredKeys, key)
		}
	}

	// Remove expired metrics
	for _, key := range expiredKeys {
		metric := c.timeSeries[key]
		delete(c.timeSeries, key)

		// Update probe index
		if probeKeys, exists := c.probeIndex[metric.ProbeName]; exists {
			delete(probeKeys, key)
			if len(probeKeys) == 0 {
				delete(c.probeIndex, metric.ProbeName)
			}
		}
	}

	if len(expiredKeys) > 0 {
		c.logger.Debug().
			Int("expired_count", len(expiredKeys)).
			Msg("💾 Cache - Cleaned up expired metrics")
	}
}

// GetAllMetrics returns all metrics currently in the cache
func (c *MetricCache) GetAllMetrics() []CachedMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := make([]CachedMetric, 0, len(c.timeSeries))
	for _, metric := range c.timeSeries {
		metrics = append(metrics, metric)
	}

	return metrics
}

// GetProbeMetrics returns all metrics for a specific probe
func (c *MetricCache) GetProbeMetrics(probeName string) []CachedMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := make([]CachedMetric, 0)

	if keys, exists := c.probeIndex[probeName]; exists {
		for key := range keys {
			if metric, exists := c.timeSeries[key]; exists {
				metrics = append(metrics, metric)
			}
		}
	}

	return metrics
}

// GetCacheInfo returns cache statistics
func (c *MetricCache) GetCacheInfo() CacheInfoResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheInfoResponse{
		TotalMetrics: len(c.timeSeries),
		ProbeCount:   len(c.probeIndex),
		TTL:          formatTTL(c.ttl),
	}
}

// formatTTL formats a duration into a human-readable string
func formatTTL(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%d hour%s %d minute%s", hours, pluralize(hours), minutes, pluralize(minutes))
		}
		return fmt.Sprintf("%d hour%s", hours, pluralize(hours))
	}

	if minutes > 0 {
		if seconds > 0 && minutes < 5 { // Show seconds for short durations
			return fmt.Sprintf("%d minute%s %d second%s", minutes, pluralize(minutes), seconds, pluralize(seconds))
		}
		return fmt.Sprintf("%d minute%s", minutes, pluralize(minutes))
	}

	return fmt.Sprintf("%d second%s", seconds, pluralize(seconds))
}

// pluralize returns "s" if count != 1, otherwise empty string
func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// ProbeStatistics represents statistics for a single probe
type ProbeStatistics struct {
	Name         string    `json:"name"`
	MetricsCount int       `json:"metrics_count"`
	LastUpdate   time.Time `json:"last_update"`
}

// GetProbeStatistics returns statistics for each probe
func (c *MetricCache) GetProbeStatistics() map[string]ProbeStatistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	probeStats := make(map[string]ProbeStatistics)

	for probeName, tsKeys := range c.probeIndex {
		if probeName == "" {
			probeName = "unknown"
		}

		metricCount := len(tsKeys)
		var lastUpdate time.Time

		// Track latest update time for each probe
		for tsKey := range tsKeys {
			if metric, exists := c.timeSeries[tsKey]; exists {
				if lastUpdate.IsZero() || metric.Timestamp.After(lastUpdate) {
					lastUpdate = metric.Timestamp
				}
			}
		}

		probeStats[probeName] = ProbeStatistics{
			Name:         probeName,
			MetricsCount: metricCount,
			LastUpdate:   lastUpdate,
		}
	}

	return probeStats
}

// GetDebugInfo returns detailed cache information for debugging
func (c *MetricCache) GetDebugInfo() DebugCacheResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := make([]DebugCacheEntry, 0, len(c.timeSeries))
	summary := make(map[string]int)

	for key, metric := range c.timeSeries {
		entries = append(entries, DebugCacheEntry{
			Key:       key,
			Name:      metric.MetricName,
			Value:     metric.Value,
			Timestamp: metric.Timestamp,
			Unit:      metric.Unit,
			ProbeName: metric.ProbeName,
			Tags:      metric.Tags,
			Age:       time.Since(metric.Timestamp).String(),
		})

		// Build summary by probe
		summary[metric.ProbeName]++
	}

	return DebugCacheResponse{
		TotalEntries: len(c.timeSeries),
		CacheTTL:     c.ttl.String(),
		Entries:      entries,
		Summary:      summary,
	}
}

// StartCleanupRoutine starts the background cleanup goroutine. The
// stop channel is re-made on every call: the HTTP strategy restarts
// the server (Shutdown → Start) on a port or bind-address change, and
// a single construction-time channel left the cleanup goroutine dead
// after the first restart (unbounded cache growth) and panicked
// (close of closed channel) on the second (#270). Idempotent: a call
// while the routine is already running is a no-op.
func (c *MetricCache) StartCleanupRoutine() {
	c.mu.Lock()
	if c.stopChan != nil {
		c.mu.Unlock()
		return
	}
	c.stopChan = make(chan struct{})
	stop := c.stopChan
	interval := c.ttl / 2 // Cleanup every half TTL
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.cleanup()
			case <-stop:
				return
			}
		}
	}()
}

// Stop stops the cache cleanup routine. Idempotent.
func (c *MetricCache) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopChan != nil {
		close(c.stopChan)
		c.stopChan = nil
	}
}

// UpdateTTL updates the cache TTL dynamically
func (c *MetricCache) UpdateTTL(newTTL time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldTTL := c.ttl
	c.ttl = newTTL

	c.logger.Info().
		Dur("old_ttl", oldTTL).
		Dur("new_ttl", newTTL).
		Msg("🔄 Cache TTL updated dynamically")
}

// CacheInfoResponse represents cache statistics
type CacheInfoResponse struct {
	TotalMetrics int    `json:"total_metrics"`
	ProbeCount   int    `json:"probe_count"`
	TTL          string `json:"ttl"`
	MemoryUsage  string `json:"memory_usage"`
}

// DebugCacheEntry represents a single cache entry for debugging
type DebugCacheEntry struct {
	Key       string            `json:"key"`
	Name      string            `json:"name"`
	Value     interface{}       `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Unit      string            `json:"unit"`
	ProbeName string            `json:"probe_name"`
	Tags      map[string]string `json:"tags"`
	Age       string            `json:"age"`
}

// DebugCacheResponse represents the complete cache state for debugging
type DebugCacheResponse struct {
	TotalEntries int               `json:"total_entries"`
	CacheTTL     string            `json:"cache_ttl"`
	Entries      []DebugCacheEntry `json:"entries"`
	Summary      map[string]int    `json:"summary"`
}

// Helper function to join strings (since we removed dependency on strings.Join)
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}

	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

// GetStatistics returns cache statistics for administration
func (c *MetricCache) GetStatistics() CacheStatistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CacheStatistics{
		TotalMetrics: len(c.timeSeries),
		Probes:       make([]ProbeStatistics, 0),
	}

	// Count metrics per probe
	probeMetrics := make(map[string]int)
	lastUpdated := make(map[string]time.Time)

	for _, metric := range c.timeSeries {
		probeMetrics[metric.ProbeName]++
		if existing, ok := lastUpdated[metric.ProbeName]; !ok || metric.Timestamp.After(existing) {
			lastUpdated[metric.ProbeName] = metric.Timestamp
		}
	}

	// Create probe statistics
	for probeName, count := range probeMetrics {
		stats.Probes = append(stats.Probes, ProbeStatistics{
			Name:         probeName,
			MetricsCount: count,
			LastUpdate:   lastUpdated[probeName],
		})
	}

	return stats
}

// Clear removes all cached metrics
func (c *MetricCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info().
		Int("cleared_metrics", len(c.timeSeries)).
		Msg("Clearing all cached metrics")

	// Clear all data structures
	c.timeSeries = make(map[string]CachedMetric)
	c.probeIndex = make(map[string]map[string]bool)
}

// CacheStatistics represents cache statistics for administration
type CacheStatistics struct {
	TotalMetrics int               `json:"total_metrics"`
	Probes       []ProbeStatistics `json:"probes"`
}
