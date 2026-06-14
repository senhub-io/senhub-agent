// Package activemq implements the free activemq probe: monitors Apache
// ActiveMQ brokers via the Jolokia HTTP REST API. Collects broker-level
// resource usage and per-destination (queue/topic) message throughput.
//
// MBeans queried:
//   - org.apache.activemq:type=Broker,brokerName=<name>  — broker overview
//   - org.apache.activemq:type=Broker,brokerName=<name>,destinationType=Queue,destinationName=*
//   - org.apache.activemq:type=Broker,brokerName=<name>,destinationType=Topic,destinationName=*
//
// Metric naming follows OTel-first convention (activemq.*); the probe status
// sentinel lives under senhub.activemq.up (gauge, 1) and is always emitted.
package activemq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

const ProbeType = "activemq"

const (
	defaultJolokiaURL = "http://localhost:8161/api/jolokia"
	defaultUsername   = "admin"
	defaultPassword   = "admin"
	defaultTimeout    = 10 * time.Second
	defaultInterval   = 60 * time.Second
)

type probeConfig struct {
	JolokiaURL  string
	Username    string
	Password    string
	Timeout     time.Duration
	Interval    time.Duration
	QueueFilter []string
	BrokerName  string
}

type activemqProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *jolokiaClient
}

// NewActivemqProbe constructs the probe. Configuration errors surface here.
func NewActivemqProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.activemq")

	cfg := probeConfig{
		JolokiaURL: defaultJolokiaURL,
		Username:   defaultUsername,
		Password:   defaultPassword,
		Timeout:    defaultTimeout,
		Interval:   defaultInterval,
		BrokerName: "localhost",
	}

	if v, ok := config["jolokia_url"].(string); ok && v != "" {
		cfg.JolokiaURL = v
	}
	if v, ok := config["username"].(string); ok && v != "" {
		cfg.Username = v
	}
	if v, ok := config["password"].(string); ok && v != "" {
		cfg.Password = v
	}
	if v, ok := config["broker_name"].(string); ok && v != "" {
		cfg.BrokerName = v
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	switch raw := config["queue_filter"].(type) {
	case []interface{}:
		for _, item := range raw {
			if s, ok := item.(string); ok && s != "" {
				cfg.QueueFilter = append(cfg.QueueFilter, s)
			}
		}
	case []string:
		cfg.QueueFilter = raw
	}

	transport := &http.Transport{}
	httpClient := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
	}
	if cfg.Username != "" {
		httpClient.Transport = &basicAuthTransport{
			username: cfg.Username,
			password: cfg.Password,
			inner:    transport,
		}
	}

	probe := &activemqProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &jolokiaClient{
			baseURL: cfg.JolokiaURL,
			http:    httpClient,
		},
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

// basicAuthTransport wraps an http.RoundTripper to inject HTTP Basic Auth.
type basicAuthTransport struct {
	username string
	password string
	inner    http.RoundTripper
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.SetBasicAuth(t.username, t.password)
	return t.inner.RoundTrip(req2)
}

func (p *activemqProbe) ShouldStart() bool          { return true }
func (p *activemqProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *activemqProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("jolokia_url", p.cfg.JolokiaURL).
		Str("broker", p.cfg.BrokerName).
		Msg("Starting activemq probe")
	return nil
}

func (p *activemqProbe) OnShutdown(_ context.Context) error {
	p.client.http.CloseIdleConnections()
	return nil
}

// Collect queries the broker and emits all metrics. A broker that cannot
// be reached still emits senhub.activemq.up=0 and returns nil (the error
// is logged, not propagated — it is a measurement, not a collection failure).
func (p *activemqProbe) Collect() ([]data_store.DataPoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	now := time.Now()
	var points []data_store.DataPoint

	brokerPoints, err := p.collectBroker(ctx, now)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("activemq broker query failed")
		points = append(points, data_store.DataPoint{
			Name:      "senhub.activemq.up",
			Value:     0,
			Timestamp: now,
			Tags:      p.baseTags("status"),
		})
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}
	points = append(points, brokerPoints...)

	queuePoints, err := p.collectDestinations(ctx, now, "Queue")
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("type", "Queue").Msg("activemq destination query failed")
	} else {
		points = append(points, queuePoints...)
	}

	topicPoints, err := p.collectDestinations(ctx, now, "Topic")
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("type", "Topic").Msg("activemq destination query failed")
	} else {
		points = append(points, topicPoints...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *activemqProbe) baseTags(metricType string) []tags.Tag {
	return []tags.Tag{
		{Key: "metric_type", Value: metricType},
		{Key: "instance", Value: p.cfg.JolokiaURL},
		{Key: "broker", Value: p.cfg.BrokerName},
	}
}

func (p *activemqProbe) brokerMBean() string {
	return fmt.Sprintf(
		"org.apache.activemq:type=Broker,brokerName=%s",
		p.cfg.BrokerName,
	)
}

func (p *activemqProbe) collectBroker(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	mbean := p.brokerMBean()

	type brokerAttr struct {
		name      string
		attribute string
		isFloat   bool
	}

	intAttrs := []struct {
		metric    string
		attribute string
	}{
		{"activemq.producer.count", "TotalProducerCount"},
		{"activemq.consumer.count", "TotalConsumerCount"},
		{"activemq.message.current", "TotalMessageCount"},
	}
	floatAttrs := []struct {
		metric    string
		attribute string
		scale     float64
	}{
		{"activemq.memory.usage", "MemoryPercentUsage", 0.01},
		{"activemq.store.usage", "StorePercentUsage", 0.01},
		{"activemq.temp.usage", "TempPercentUsage", 0.01},
	}

	_ = brokerAttr{}

	statusTags := p.baseTags("status")
	brokerTags := p.baseTags("overview")

	var points []data_store.DataPoint

	// Validate broker is reachable by reading one attribute first.
	producerCount, err := p.client.readInt64(ctx, mbean, "TotalProducerCount")
	if err != nil {
		return nil, fmt.Errorf("reading TotalProducerCount: %w", err)
	}

	points = append(points, data_store.DataPoint{
		Name:      "senhub.activemq.up",
		Value:     1,
		Timestamp: now,
		Tags:      statusTags,
	})

	points = append(points, data_store.DataPoint{
		Name:      "activemq.producer.count",
		Value:     float32(producerCount),
		Timestamp: now,
		Tags:      brokerTags,
	})

	for _, a := range intAttrs[1:] { // skip TotalProducerCount already read
		v, err := p.client.readInt64(ctx, mbean, a.attribute)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("attr", a.attribute).Msg("broker attribute read failed")
			continue
		}
		points = append(points, data_store.DataPoint{
			Name:      a.metric,
			Value:     float32(v),
			Timestamp: now,
			Tags:      brokerTags,
		})
	}

	for _, a := range floatAttrs {
		v, err := p.client.readFloat64(ctx, mbean, a.attribute)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("attr", a.attribute).Msg("broker attribute read failed")
			continue
		}
		points = append(points, data_store.DataPoint{
			Name:      a.metric,
			Value:     float32(v * a.scale),
			Timestamp: now,
			Tags:      brokerTags,
		})
	}

	return points, nil
}

// listDestinationNames queries the Jolokia list endpoint for all known
// destination names of a given type (Queue or Topic). Returns the names
// after applying the queue_filter glob list (if configured).
func (p *activemqProbe) listDestinationNames(ctx context.Context, destType string) ([]string, error) {
	path := fmt.Sprintf(
		"%s/list/org.apache.activemq%%3Atype%%3DBroker,brokerName%%3D%s,destinationType%%3D%s",
		p.cfg.JolokiaURL,
		p.cfg.BrokerName,
		destType,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if p.cfg.Username != "" {
		req.SetBasicAuth(p.cfg.Username, p.cfg.Password)
	}
	resp, err := p.client.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	// Jolokia list response: {"value": {"<destName>": {...}}, "status": 200}
	var jr struct {
		Value  map[string]json.RawMessage `json:"value"`
		Status int                        `json:"status"`
		Error  string                     `json:"error"`
	}
	if err := json.Unmarshal(body, &jr); err != nil {
		return nil, fmt.Errorf("jolokia list parse: %w", err)
	}
	if jr.Status != 200 {
		return nil, fmt.Errorf("jolokia list error: %s", jr.Error)
	}

	var names []string
	for name := range jr.Value {
		if !p.matchesFilter(name) {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// matchesFilter returns true when name matches at least one glob in
// queue_filter, or when queue_filter is empty (pass-all).
func (p *activemqProbe) matchesFilter(name string) bool {
	if len(p.cfg.QueueFilter) == 0 {
		return true
	}
	for _, glob := range p.cfg.QueueFilter {
		if ok, _ := filepath.Match(glob, name); ok {
			return true
		}
	}
	return false
}

func (p *activemqProbe) collectDestinations(ctx context.Context, now time.Time, destType string) ([]data_store.DataPoint, error) {
	names, err := p.listDestinationNames(ctx, destType)
	if err != nil {
		return nil, fmt.Errorf("listing %s destinations: %w", destType, err)
	}

	destTypeTag := "queue"
	if destType == "Topic" {
		destTypeTag = "topic"
	}

	var points []data_store.DataPoint
	for _, name := range names {
		pts := p.collectOneDestination(ctx, now, destType, name, destTypeTag)
		points = append(points, pts...)
	}
	return points, nil
}

func (p *activemqProbe) collectOneDestination(
	ctx context.Context,
	now time.Time,
	destType, destName, destTypeTag string,
) []data_store.DataPoint {
	mbean := fmt.Sprintf(
		"org.apache.activemq:type=Broker,brokerName=%s,destinationType=%s,destinationName=%s",
		p.cfg.BrokerName, destType, destName,
	)

	destTags := append(p.baseTags("messaging"), tags.Tag{
		Key: "destination", Value: destName,
	}, tags.Tag{
		Key: "destination_type", Value: destTypeTag,
	})

	type intMetric struct {
		metric    string
		attribute string
	}
	attrs := []intMetric{
		{"activemq.message.enqueued", "EnqueueCount"},
		{"activemq.message.dequeued", "DequeueCount"},
		{"activemq.message.queue_size", "QueueSize"},
		{"activemq.consumer.count", "ConsumerCount"},
		{"activemq.producer.count", "ProducerCount"},
	}

	var points []data_store.DataPoint
	for _, a := range attrs {
		v, err := p.client.readInt64(ctx, mbean, a.attribute)
		if err != nil {
			p.moduleLogger.Warn().
				Err(err).
				Str("destination", destName).
				Str("attr", a.attribute).
				Msg("destination attribute read failed")
			continue
		}
		points = append(points, data_store.DataPoint{
			Name:      a.metric,
			Value:     float32(v),
			Timestamp: now,
			Tags:      destTags,
		})
	}
	return points
}
