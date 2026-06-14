// Package opensearch implements the FREE-tier OpenSearch monitoring probe.
// It queries /_cluster/health and /_nodes/_local/stats over the REST JSON API.
// OpenSearch exposes the same REST API surface as Elasticsearch, so parsing is
// identical; only the probe slug, metric names, and logger prefix differ.
package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable identifier used in YAML, JWT, registry.
const ProbeType = "opensearch"

const (
	defaultEndpoint = "http://localhost:9200"
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second
)

type opensearchProbe struct {
	*types.BaseProbe
	cfg          osConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
}

type osConfig struct {
	Endpoint string
	Username string
	Password string
	Interval time.Duration
	Timeout  time.Duration
}

// NewOpenSearchProbe constructs the probe from the agent YAML params.
func NewOpenSearchProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.opensearch")

	cfg := osConfig{
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

	probe := &opensearchProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client:       &http.Client{Timeout: cfg.Timeout},
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func (p *opensearchProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *opensearchProbe) ShouldStart() bool          { return true }
func (p *opensearchProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *opensearchProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting opensearch probe")
	return nil
}

func (p *opensearchProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches cluster health + local node stats and emits datapoints.
// senhub.opensearch.up is always emitted, even on error (up=0).
func (p *opensearchProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	var points []data_store.DataPoint

	upTags := []tags.Tag{{Key: "metric_type", Value: "status"}}

	health, err := p.fetchClusterHealth()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("opensearch cluster health fetch failed")
		points = append(points,
			data_store.DataPoint{Name: "senhub.opensearch.up", Value: 0, Timestamp: now, Tags: upTags},
		)
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	points = append(points,
		data_store.DataPoint{Name: "senhub.opensearch.up", Value: 1, Timestamp: now, Tags: upTags},
	)
	points = append(points, p.buildClusterHealthPoints(health, now)...)

	nodeStats, err := p.fetchNodeStats()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("opensearch node stats fetch failed")
		// Return what we have (cluster health is available).
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}
	points = append(points, p.buildNodeStatsPoints(nodeStats, now)...)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
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

func (p *opensearchProbe) fetchClusterHealth() (*clusterHealth, error) {
	var h clusterHealth
	if err := p.getJSON("/_cluster/health", &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func statusToFloat(status string) float32 {
	switch status {
	case "green":
		return 2
	case "yellow":
		return 1
	default: // "red" or unknown
		return 0
	}
}

func (p *opensearchProbe) buildClusterHealthPoints(h *clusterHealth, ts time.Time) []data_store.DataPoint {
	clusterTag := tags.Tag{Key: "metric_type", Value: "status"}
	nodeTag := tags.Tag{Key: "metric_type", Value: "nodes"}
	shardTag := tags.Tag{Key: "metric_type", Value: "shards"}
	taskTag := tags.Tag{Key: "metric_type", Value: "tasks"}

	return []data_store.DataPoint{
		{Name: "opensearch.cluster.health", Value: statusToFloat(h.Status), Timestamp: ts, Tags: []tags.Tag{clusterTag}},
		{Name: "opensearch.cluster.nodes", Value: float32(h.NumberOfNodes), Timestamp: ts, Tags: []tags.Tag{nodeTag}},
		{Name: "opensearch.cluster.data_nodes", Value: float32(h.NumberOfDataNodes), Timestamp: ts, Tags: []tags.Tag{nodeTag}},
		{Name: "opensearch.cluster.shards.active", Value: float32(h.ActiveShards), Timestamp: ts, Tags: []tags.Tag{shardTag}},
		{Name: "opensearch.cluster.shards.unassigned", Value: float32(h.UnassignedShards), Timestamp: ts, Tags: []tags.Tag{shardTag}},
		{Name: "opensearch.cluster.shards.relocating", Value: float32(h.RelocatingShards), Timestamp: ts, Tags: []tags.Tag{shardTag}},
		{Name: "opensearch.cluster.pending_tasks", Value: float32(h.NumberOfPendingTasks), Timestamp: ts, Tags: []tags.Tag{taskTag}},
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

func (p *opensearchProbe) fetchNodeStats() (*nodeStatsResponse, error) {
	var r nodeStatsResponse
	if err := p.getJSON("/_nodes/_local/stats", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (p *opensearchProbe) buildNodeStatsPoints(r *nodeStatsResponse, ts time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	for _, node := range r.Nodes {
		jvmTags := []tags.Tag{{Key: "metric_type", Value: "jvm"}}
		points = append(points,
			data_store.DataPoint{
				Name:      "opensearch.jvm.memory.heap.used",
				Value:     float32(node.JVM.Mem.HeapUsedInBytes),
				Timestamp: ts,
				Tags:      jvmTags,
			},
			data_store.DataPoint{
				Name:      "opensearch.jvm.memory.heap.max",
				Value:     float32(node.JVM.Mem.HeapMaxInBytes),
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
					Name:      "opensearch.jvm.gc.collections.count",
					Value:     float32(gc.CollectionCount),
					Timestamp: ts,
					Tags:      gcTags,
				},
				data_store.DataPoint{
					Name:      "opensearch.jvm.gc.collections.elapsed",
					Value:     float32(gc.CollectionTimeInMillis),
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
				Name:      "opensearch.indexing.operations.completed",
				Value:     float32(node.Indices.Indexing.IndexTotal),
				Timestamp: ts,
				Tags:      indexingTags,
			},
			data_store.DataPoint{
				Name:      "opensearch.indexing.operations.time",
				Value:     float32(node.Indices.Indexing.IndexTimeInMillis),
				Timestamp: ts,
				Tags:      indexingTags,
			},
			data_store.DataPoint{
				Name:      "opensearch.search.operations.completed",
				Value:     float32(node.Indices.Search.QueryTotal),
				Timestamp: ts,
				Tags:      searchQueryTags,
			},
			data_store.DataPoint{
				Name:      "opensearch.search.operations.time",
				Value:     float32(node.Indices.Search.QueryTimeInMillis),
				Timestamp: ts,
				Tags:      searchQueryTags,
			},
			data_store.DataPoint{
				Name:      "opensearch.search.operations.completed",
				Value:     float32(node.Indices.Search.FetchTotal),
				Timestamp: ts,
				Tags:      searchFetchTags,
			},
			data_store.DataPoint{
				Name:      "opensearch.search.operations.time",
				Value:     float32(node.Indices.Search.FetchTimeInMillis),
				Timestamp: ts,
				Tags:      searchFetchTags,
			},
		)

		processTags := []tags.Tag{{Key: "metric_type", Value: "process"}}
		points = append(points,
			data_store.DataPoint{
				Name:      "opensearch.process.cpu.usage",
				Value:     float32(node.Process.CPU.Percent) / 100,
				Timestamp: ts,
				Tags:      processTags,
			},
		)

		osTags := []tags.Tag{{Key: "metric_type", Value: "os"}}
		points = append(points,
			data_store.DataPoint{
				Name:      "opensearch.os.memory.used",
				Value:     float32(node.OS.Mem.UsedInBytes),
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
					Name:      "opensearch.thread_pool.tasks.queued",
					Value:     float32(pool.Queue),
					Timestamp: ts,
					Tags:      tpTags,
				},
				data_store.DataPoint{
					Name:      "opensearch.thread_pool.tasks.completed",
					Value:     float32(pool.Completed),
					Timestamp: ts,
					Tags:      tpTags,
				},
				data_store.DataPoint{
					Name:      "opensearch.thread_pool.tasks.rejected",
					Value:     float32(pool.Rejected),
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

func (p *opensearchProbe) getJSON(path string, v interface{}) error {
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
