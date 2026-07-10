// Package elasticsearch implements the FREE-tier Elasticsearch monitoring probe.
// It queries /_cluster/health and /_nodes/_local/stats over the REST JSON API
// and emits OTel-first metrics aligned with the otelcol-contrib
// elasticsearchreceiver where equivalents exist.
package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable identifier used in YAML, JWT, registry.
const ProbeType = "elasticsearch"

const (
	defaultEndpoint = "http://localhost:9200"
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second
)

type elasticsearchProbe struct {
	*types.BaseProbe
	cfg          esConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	// entitySrc feeds the Toise topology inventory (search.engine entity).
	entitySrc  *elasticsearchEntitySource
	unregister func()
}

type esConfig struct {
	Endpoint     string
	Username     string
	Password     string
	InstanceName string
	Interval     time.Duration
	Timeout      time.Duration
}

// NewElasticsearchProbe constructs the probe from the agent YAML params.
func NewElasticsearchProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.elasticsearch")

	cfg := esConfig{
		Endpoint: defaultEndpoint,
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
	}
	if v, ok := config["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := config["password"].(string); ok {
		cfg.Password = v
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

	host, port := hostPortFromEndpoint(cfg.Endpoint)
	probe := &elasticsearchProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client:       &http.Client{Timeout: cfg.Timeout},
	}
	probe.SetProbeType(ProbeType)
	probe.entitySrc = newElasticsearchEntitySource(cfg.InstanceName, host, port)
	probe.SetEntitySource(probe.entitySrc)
	return probe, nil
}

func (p *elasticsearchProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *elasticsearchProbe) ShouldStart() bool          { return true }
func (p *elasticsearchProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *elasticsearchProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting elasticsearch probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *elasticsearchProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches cluster health + local node stats and emits datapoints.
// senhub.elasticsearch.up is always emitted, even on error (up=0).
// On the first successful collect it also calls GET / to pin the cluster_uuid
// used as the stable db.instance.id in the entity rail.
func (p *elasticsearchProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	var points []data_store.DataPoint

	upTags := []tags.Tag{{Key: "metric_type", Value: "status"}}

	health, err := p.fetchClusterHealth()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("elasticsearch cluster health fetch failed")
		p.entitySrc.setReachable(false)
		points = append(points,
			data_store.DataPoint{Name: "senhub.elasticsearch.up", Value: 0, Timestamp: now, Tags: upTags},
		)
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	// Pin the stable cluster id on first successful reach.  The entity source
	// suppresses the db entity until this is called at least once.
	p.maybePin()

	p.entitySrc.setReachable(true)

	points = append(points,
		data_store.DataPoint{Name: "senhub.elasticsearch.up", Value: 1, Timestamp: now, Tags: upTags},
	)
	points = append(points, p.buildClusterHealthPoints(health, now)...)

	nodeStats, err := p.fetchNodeStats()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("elasticsearch node stats fetch failed")
		// Return what we have (cluster health is available).
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}
	points = append(points, p.buildNodeStatsPoints(nodeStats, now)...)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// ----- root info (cluster_uuid + version) ------------------------------------

// rootInfo is the response shape for GET /.
type rootInfo struct {
	ClusterUUID string `json:"cluster_uuid"`
	Version     struct {
		Number string `json:"number"`
	} `json:"version"`
}

// maybePin fetches GET / to pin the cluster_uuid used as the stable
// db.instance.id.  It is a no-op once the entity source has been pinned
// (either by instance_name at construction or by a prior call here).
// Errors are logged at debug level and silently ignored: the entity is simply
// not emitted until a successful fetch pins the id.
func (p *elasticsearchProbe) maybePin() {
	p.entitySrc.mu.RLock()
	already := p.entitySrc.pinned
	p.entitySrc.mu.RUnlock()
	if already {
		return
	}

	var info rootInfo
	if err := p.getJSON("/", &info); err != nil {
		p.moduleLogger.Debug().Err(err).Msg("elasticsearch root fetch failed; cluster_uuid pending")
		return
	}
	p.entitySrc.pinClusterUUID(info.ClusterUUID)
	p.entitySrc.updateVersion(info.Version.Number)
}

// ----- cluster health ---------------------------------------------------------

type clusterHealth struct {
	ClusterName          string `json:"cluster_name"`
	Status               string `json:"status"`
	NumberOfNodes        int    `json:"number_of_nodes"`
	NumberOfDataNodes    int    `json:"number_of_data_nodes"`
	ActiveShards         int    `json:"active_shards"`
	UnassignedShards     int    `json:"unassigned_shards"`
	RelocatingShards     int    `json:"relocating_shards"`
	NumberOfPendingTasks int    `json:"number_of_pending_tasks"`
}

func (p *elasticsearchProbe) fetchClusterHealth() (*clusterHealth, error) {
	var h clusterHealth
	if err := p.getJSON("/_cluster/health", &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func statusToFloat(status string) float64 {
	switch status {
	case "green":
		return 2
	case "yellow":
		return 1
	default: // "red" or unknown
		return 0
	}
}

func (p *elasticsearchProbe) buildClusterHealthPoints(h *clusterHealth, ts time.Time) []data_store.DataPoint {
	clusterTag := tags.Tag{Key: "metric_type", Value: "status"}
	nodeTag := tags.Tag{Key: "metric_type", Value: "nodes"}
	shardTag := tags.Tag{Key: "metric_type", Value: "shards"}
	taskTag := tags.Tag{Key: "metric_type", Value: "tasks"}

	return []data_store.DataPoint{
		{Name: "elasticsearch.cluster.health", Value: statusToFloat(h.Status), Timestamp: ts, Tags: []tags.Tag{clusterTag}},
		{Name: "elasticsearch.cluster.nodes", Value: float64(h.NumberOfNodes), Timestamp: ts, Tags: []tags.Tag{nodeTag}},
		{Name: "elasticsearch.cluster.data_nodes", Value: float64(h.NumberOfDataNodes), Timestamp: ts, Tags: []tags.Tag{nodeTag}},
		{Name: "elasticsearch.cluster.shards.active", Value: float64(h.ActiveShards), Timestamp: ts, Tags: []tags.Tag{shardTag}},
		{Name: "elasticsearch.cluster.shards.unassigned", Value: float64(h.UnassignedShards), Timestamp: ts, Tags: []tags.Tag{shardTag}},
		{Name: "elasticsearch.cluster.shards.relocating", Value: float64(h.RelocatingShards), Timestamp: ts, Tags: []tags.Tag{shardTag}},
		{Name: "elasticsearch.cluster.pending_tasks", Value: float64(h.NumberOfPendingTasks), Timestamp: ts, Tags: []tags.Tag{taskTag}},
	}
}

// ----- node stats -------------------------------------------------------------

// nodeStatsResponse is the envelope for /_nodes/_local/stats.
type nodeStatsResponse struct {
	Nodes map[string]nodeStats `json:"nodes"`
}

type nodeStats struct {
	Name       string                     `json:"name"`
	JVM        jvmStats                   `json:"jvm"`
	Process    processStats               `json:"process"`
	OS         osStats                    `json:"os"`
	Indices    indexStats                 `json:"indices"`
	ThreadPool map[string]threadPoolStats `json:"thread_pool"`
}

type jvmStats struct {
	Mem struct {
		HeapUsedInBytes int64 `json:"heap_used_in_bytes"`
		HeapMaxInBytes  int64 `json:"heap_max_in_bytes"`
	} `json:"mem"`
	GC struct {
		Collectors map[string]gcCollector `json:"collectors"`
	} `json:"gc"`
}

type gcCollector struct {
	CollectionCount        int64 `json:"collection_count"`
	CollectionTimeInMillis int64 `json:"collection_time_in_millis"`
}

type processStats struct {
	CPU struct {
		Percent int `json:"percent"`
	} `json:"cpu"`
}

type osStats struct {
	Mem struct {
		UsedInBytes int64 `json:"used_in_bytes"`
	} `json:"mem"`
}

type indexStats struct {
	Indexing struct {
		IndexTotal        int64 `json:"index_total"`
		IndexTimeInMillis int64 `json:"index_time_in_millis"`
	} `json:"indexing"`
	Search struct {
		QueryTotal        int64 `json:"query_total"`
		QueryTimeInMillis int64 `json:"query_time_in_millis"`
		FetchTotal        int64 `json:"fetch_total"`
		FetchTimeInMillis int64 `json:"fetch_time_in_millis"`
	} `json:"search"`
}

type threadPoolStats struct {
	Queue     int   `json:"queue"`
	Completed int64 `json:"completed"`
	Rejected  int64 `json:"rejected"`
}

func (p *elasticsearchProbe) fetchNodeStats() (*nodeStatsResponse, error) {
	var r nodeStatsResponse
	if err := p.getJSON("/_nodes/_local/stats", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (p *elasticsearchProbe) buildNodeStatsPoints(r *nodeStatsResponse, ts time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	for _, node := range r.Nodes {
		jvmTags := []tags.Tag{{Key: "metric_type", Value: "jvm"}}
		points = append(points,
			data_store.DataPoint{
				Name:      "elasticsearch.jvm.memory.heap.used",
				Value:     float64(node.JVM.Mem.HeapUsedInBytes),
				Timestamp: ts,
				Tags:      jvmTags,
			},
			data_store.DataPoint{
				Name:      "elasticsearch.jvm.memory.heap.max",
				Value:     float64(node.JVM.Mem.HeapMaxInBytes),
				Timestamp: ts,
				Tags:      jvmTags,
			},
		)

		for collectorName, gc := range node.JVM.GC.Collectors {
			gcTags := []tags.Tag{
				{Key: "metric_type", Value: "jvm"},
				{Key: "collector", Value: collectorName},
			}
			points = append(points,
				data_store.DataPoint{
					Name:      "elasticsearch.jvm.gc.collections.count",
					Value:     float64(gc.CollectionCount),
					Timestamp: ts,
					Tags:      gcTags,
				},
				data_store.DataPoint{
					Name:      "elasticsearch.jvm.gc.collections.elapsed",
					Value:     float64(gc.CollectionTimeInMillis),
					Timestamp: ts,
					Tags:      gcTags,
				},
			)
		}

		indexingTags := []tags.Tag{
			{Key: "metric_type", Value: "indexing"},
			{Key: "operation", Value: "index"},
		}
		searchQueryTags := []tags.Tag{
			{Key: "metric_type", Value: "search"},
			{Key: "operation", Value: "query"},
		}
		searchFetchTags := []tags.Tag{
			{Key: "metric_type", Value: "search"},
			{Key: "operation", Value: "fetch"},
		}

		points = append(points,
			data_store.DataPoint{
				Name:      "elasticsearch.indexing.operations.completed",
				Value:     float64(node.Indices.Indexing.IndexTotal),
				Timestamp: ts,
				Tags:      indexingTags,
			},
			data_store.DataPoint{
				Name:      "elasticsearch.indexing.operations.time",
				Value:     float64(node.Indices.Indexing.IndexTimeInMillis),
				Timestamp: ts,
				Tags:      indexingTags,
			},
			data_store.DataPoint{
				Name:      "elasticsearch.search.operations.completed",
				Value:     float64(node.Indices.Search.QueryTotal),
				Timestamp: ts,
				Tags:      searchQueryTags,
			},
			data_store.DataPoint{
				Name:      "elasticsearch.search.operations.time",
				Value:     float64(node.Indices.Search.QueryTimeInMillis),
				Timestamp: ts,
				Tags:      searchQueryTags,
			},
			data_store.DataPoint{
				Name:      "elasticsearch.search.operations.completed",
				Value:     float64(node.Indices.Search.FetchTotal),
				Timestamp: ts,
				Tags:      searchFetchTags,
			},
			data_store.DataPoint{
				Name:      "elasticsearch.search.operations.time",
				Value:     float64(node.Indices.Search.FetchTimeInMillis),
				Timestamp: ts,
				Tags:      searchFetchTags,
			},
		)

		processTags := []tags.Tag{{Key: "metric_type", Value: "process"}}
		points = append(points,
			data_store.DataPoint{
				Name:      "elasticsearch.process.cpu.usage",
				Value:     float64(node.Process.CPU.Percent),
				Timestamp: ts,
				Tags:      processTags,
			},
		)

		osTags := []tags.Tag{{Key: "metric_type", Value: "os"}}
		points = append(points,
			data_store.DataPoint{
				Name:      "elasticsearch.os.memory.used",
				Value:     float64(node.OS.Mem.UsedInBytes),
				Timestamp: ts,
				Tags:      osTags,
			},
		)

		for poolName, pool := range node.ThreadPool {
			tpTags := []tags.Tag{
				{Key: "metric_type", Value: "thread_pool"},
				{Key: "thread_pool", Value: poolName},
			}
			points = append(points,
				data_store.DataPoint{
					Name:      "elasticsearch.thread_pool.tasks.queued",
					Value:     float64(pool.Queue),
					Timestamp: ts,
					Tags:      tpTags,
				},
				data_store.DataPoint{
					Name:      "elasticsearch.thread_pool.tasks.completed",
					Value:     float64(pool.Completed),
					Timestamp: ts,
					Tags:      tpTags,
				},
				data_store.DataPoint{
					Name:      "elasticsearch.thread_pool.tasks.rejected",
					Value:     float64(pool.Rejected),
					Timestamp: ts,
					Tags:      tpTags,
				},
			)
		}

		// Only the first node is processed for single-node installs via
		// /_nodes/_local/stats. Multi-node installs should switch to
		// /_nodes/stats and will produce one set of metrics per node
		// (break here so single-node installs are not double-counted).
		break
	}

	return points
}

// ----- HTTP helpers -----------------------------------------------------------

func (p *elasticsearchProbe) getJSON(path string, v interface{}) error {
	url := p.cfg.Endpoint + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", url, err)
	}
	if p.cfg.Username != "" {
		req.SetBasicAuth(p.cfg.Username, p.cfg.Password)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decoding response from %s: %w", url, err)
	}
	return nil
}
