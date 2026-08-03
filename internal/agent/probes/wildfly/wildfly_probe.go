// Package wildfly implements the FREE-tier WildFly / JBoss probe.
// It talks to the WildFly HTTP Management API (native, no Jolokia
// required) to collect JVM memory, Undertow web-container request
// statistics, JTA transaction counters, and JDBC datasource pool
// metrics.
//
// API: POST <endpoint>/management with JSON body describing the
// operation and address within the WildFly DMR (Domain Model
// Repository).
package wildfly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "wildfly"

const (
	defaultEndpoint = "http://localhost:9990"
	defaultUsername = "admin"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second

	maxResponseBytes = 1 << 20 // 1 MiB guard
)

// mgmtRequest is the WildFly Management API JSON request body.
type mgmtRequest struct {
	Operation string              `json:"operation"`
	Address   []map[string]string `json:"address"`
	Name      string              `json:"name,omitempty"`
	// include-runtime and recursive-only apply to read-resource.
	IncludeRuntime bool `json:"include-runtime,omitempty"`
	Recursive      bool `json:"recursive,omitempty"`
}

// mgmtResponse is the envelope returned by /management.
type mgmtResponse struct {
	Outcome string          `json:"outcome"`
	Result  json.RawMessage `json:"result"`
	// failure-description is present when outcome == "failed".
	FailureDescription string `json:"failure-description,omitempty"`
}

// probeConfig holds validated configuration.
type probeConfig struct {
	Endpoint     string
	Username     string
	Password     string
	Timeout      time.Duration
	Interval     time.Duration
	InstanceName string // optional stable override for service.instance.id
}

// WildflyProbe collects metrics from a WildFly / JBoss instance via
// the HTTP Management API.
type WildflyProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *wildflyEntitySource
}

// NewWildflyProbe is the probe constructor.
func NewWildflyProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.wildfly")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	resolveHostID := func() string {
		id, err := common.GetHostIdentity()
		if err != nil {
			return ""
		}
		return id.ID
	}

	p := &WildflyProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		entitySrc: newWildflyEntitySource(cfg.Endpoint, cfg.InstanceName, resolveHostID),
	}
	p.SetProbeType(ProbeType)
	p.SetEntitySource(p.entitySrc)
	return p, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Endpoint: defaultEndpoint,
		Username: defaultUsername,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
	}
	if v, ok := config["username"].(string); ok && v != "" {
		cfg.Username = v
	}
	if v, ok := config["password"].(string); ok {
		cfg.Password = v
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
	return cfg, nil
}

func (p *WildflyProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *WildflyProbe) ShouldStart() bool          { return true }
func (p *WildflyProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *WildflyProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting wildfly probe")
	return nil
}

func (p *WildflyProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect gathers all metrics in one cycle. A connection failure is
// recorded as senhub.wildfly.up=0 but never returned as an error —
// the framework must keep scheduling even when the server is down.
func (p *WildflyProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	var points []data_store.DataPoint

	up := float64(0)
	if err := p.collectAll(ctx, now, &points); err != nil {
		p.moduleLogger.Warn().
			Err(err).
			Str("endpoint", p.cfg.Endpoint).
			Msg("wildfly collect failed")
		p.entitySrc.setReachable(false, "")
	} else {
		up = 1
		p.entitySrc.setReachable(true, "")
	}

	upPoint := data_store.DataPoint{
		Name:      "senhub.wildfly.up",
		Value:     up,
		Timestamp: now,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "availability"},
		},
	}
	points = append([]data_store.DataPoint{upPoint}, points...)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// collectAll issues all management API calls and appends the resulting
// datapoints. Any individual section failure aborts collection and
// returns the error so that up=0 is recorded.
func (p *WildflyProbe) collectAll(ctx context.Context, now time.Time, points *[]data_store.DataPoint) error {
	if err := p.collectJVM(ctx, now, points); err != nil {
		return fmt.Errorf("jvm: %w", err)
	}
	if err := p.collectUndertow(ctx, now, points); err != nil {
		return fmt.Errorf("undertow: %w", err)
	}
	if err := p.collectTransactions(ctx, now, points); err != nil {
		return fmt.Errorf("transactions: %w", err)
	}
	if err := p.collectDatasources(ctx, now, points); err != nil {
		return fmt.Errorf("datasources: %w", err)
	}
	return nil
}

// mgmtCall performs a single POST /management API call and unmarshals
// the result into dst.
func (p *WildflyProbe) mgmtCall(ctx context.Context, req mgmtRequest, dst interface{}) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := p.cfg.Endpoint + "/management"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.Username != "" && p.cfg.Password != "" {
		httpReq.SetBasicAuth(p.cfg.Username, p.cfg.Password)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed (HTTP 401)")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	var envelope mgmtResponse
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return fmt.Errorf("parse envelope: %w", err)
	}
	if envelope.Outcome != "success" {
		return fmt.Errorf("management API: %s", envelope.FailureDescription)
	}
	if dst != nil {
		if err := json.Unmarshal(envelope.Result, dst); err != nil {
			return fmt.Errorf("parse result: %w", err)
		}
	}
	return nil
}

// collectJVM reads heap memory usage from the platform-mbean subsystem.
func (p *WildflyProbe) collectJVM(ctx context.Context, now time.Time, points *[]data_store.DataPoint) error {
	req := mgmtRequest{
		Operation: "read-resource",
		Address: []map[string]string{
			{"core-service": "platform-mbean"},
			{"type": "memory"},
		},
		IncludeRuntime: true,
	}

	var result struct {
		HeapMemoryUsage struct {
			Used      int64 `json:"used"`
			Committed int64 `json:"committed"`
			Max       int64 `json:"max"`
		} `json:"heap-memory-usage"`
	}
	if err := p.mgmtCall(ctx, req, &result); err != nil {
		return err
	}

	mt := []tags.Tag{{Key: "metric_type", Value: "memory"}}
	*points = append(*points,
		data_store.DataPoint{Name: "jvm.memory.heap.used", Value: float64(result.HeapMemoryUsage.Used), Timestamp: now, Tags: mt},
		data_store.DataPoint{Name: "jvm.memory.heap.committed", Value: float64(result.HeapMemoryUsage.Committed), Timestamp: now, Tags: mt},
		data_store.DataPoint{Name: "jvm.memory.heap.max", Value: float64(result.HeapMemoryUsage.Max), Timestamp: now, Tags: mt},
	)
	return nil
}

// collectUndertow reads web-container counters from the Undertow subsystem.
func (p *WildflyProbe) collectUndertow(ctx context.Context, now time.Time, points *[]data_store.DataPoint) error {
	req := mgmtRequest{
		Operation: "read-resource",
		Address: []map[string]string{
			{"subsystem": "undertow"},
		},
		IncludeRuntime: true,
	}

	var result struct {
		RequestCount  int64 `json:"request-count"`
		ErrorCount    int64 `json:"error-count"`
		BytesSent     int64 `json:"bytes-sent"`
		BytesReceived int64 `json:"bytes-received"`
	}
	if err := p.mgmtCall(ctx, req, &result); err != nil {
		return err
	}

	mt := []tags.Tag{{Key: "metric_type", Value: "requests"}}
	*points = append(*points,
		data_store.DataPoint{Name: "wildfly.request.count", Value: float64(result.RequestCount), Timestamp: now, Tags: mt},
		data_store.DataPoint{Name: "wildfly.error.count", Value: float64(result.ErrorCount), Timestamp: now, Tags: mt},
		data_store.DataPoint{Name: "wildfly.bytes.sent", Value: float64(result.BytesSent), Timestamp: now, Tags: mt},
		data_store.DataPoint{Name: "wildfly.bytes.received", Value: float64(result.BytesReceived), Timestamp: now, Tags: mt},
	)
	return nil
}

// collectTransactions reads JTA transaction counters.
func (p *WildflyProbe) collectTransactions(ctx context.Context, now time.Time, points *[]data_store.DataPoint) error {
	req := mgmtRequest{
		Operation: "read-resource",
		Address: []map[string]string{
			{"subsystem": "transactions"},
		},
		IncludeRuntime: true,
	}

	var result struct {
		NumberOfTransactions        int64 `json:"number-of-transactions"`
		NumberOfAbortedTransactions int64 `json:"number-of-aborted-transactions"`
	}
	if err := p.mgmtCall(ctx, req, &result); err != nil {
		return err
	}

	mt := []tags.Tag{{Key: "metric_type", Value: "operations"}}
	*points = append(*points,
		data_store.DataPoint{Name: "wildfly.transaction.committed", Value: float64(result.NumberOfTransactions), Timestamp: now, Tags: mt},
		data_store.DataPoint{Name: "wildfly.transaction.rolledback", Value: float64(result.NumberOfAbortedTransactions), Timestamp: now, Tags: mt},
	)
	return nil
}

// collectDatasources iterates all configured JDBC datasources and
// collects their pool metrics, one set of datapoints per datasource.
func (p *WildflyProbe) collectDatasources(ctx context.Context, now time.Time, points *[]data_store.DataPoint) error {
	// List datasource names first.
	listReq := mgmtRequest{
		Operation: "read-children-names",
		Address: []map[string]string{
			{"subsystem": "datasources"},
		},
		Name: "data-source",
	}
	var names []string
	if err := p.mgmtCall(ctx, listReq, &names); err != nil {
		// If the datasources subsystem is absent, skip silently.
		return nil
	}

	for _, dsName := range names {
		if err := p.collectOneDatasource(ctx, now, points, dsName); err != nil {
			p.moduleLogger.Warn().
				Err(err).
				Str("datasource", dsName).
				Msg("wildfly datasource collect failed; skipping")
		}
	}
	return nil
}

func (p *WildflyProbe) collectOneDatasource(ctx context.Context, now time.Time, points *[]data_store.DataPoint, dsName string) error {
	req := mgmtRequest{
		Operation: "read-resource",
		Address: []map[string]string{
			{"subsystem": "datasources"},
			{"data-source": dsName},
		},
		IncludeRuntime: true,
	}

	var result struct {
		Statistics struct {
			Pool struct {
				ActiveCount    int64 `json:"ActiveCount"`
				AvailableCount int64 `json:"AvailableCount"`
			} `json:"pool"`
		} `json:"statistics"`
	}
	if err := p.mgmtCall(ctx, req, &result); err != nil {
		return err
	}

	mt := []tags.Tag{
		{Key: "metric_type", Value: "connections"},
		{Key: "datasource", Value: dsName},
	}
	*points = append(*points,
		data_store.DataPoint{Name: "wildfly.datasource.connections.active", Value: float64(result.Statistics.Pool.ActiveCount), Timestamp: now, Tags: mt},
		data_store.DataPoint{Name: "wildfly.datasource.connections.available", Value: float64(result.Statistics.Pool.AvailableCount), Timestamp: now, Tags: mt},
	)
	return nil
}
