// Package tomcat implements the free Apache Tomcat monitoring probe via Jolokia REST.
//
// Jolokia (https://jolokia.org/) exposes JMX MBeans over HTTP/JSON.
// The probe reads GlobalRequestProcessor, ThreadPool, Memory, GarbageCollector
// and Threading MBeans. All metric names follow OTel semantic conventions where
// available (jvm.memory.*, jvm.gc.*, jvm.threads.*) and extend under tomcat.*
// for Tomcat-specific metrics where no OTel standard exists.
package tomcat

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "tomcat"

const (
	defaultJolokiaURL = "http://localhost:8080/jolokia"
	defaultTimeout    = 10 * time.Second
	defaultInterval   = 60 * time.Second
)

// knownConnectors lists the HTTP connector names Tomcat exposes by default.
// The probe attempts to collect metrics for each connector that exists; absent
// connectors are silently skipped (Jolokia returns an error status for unknown
// MBeans, which the collector treats as "not configured").
var knownConnectors = []string{"http-nio-8080", "http-nio-8443"}

// knownGCCollectors lists the common G1 GC collector names in HotSpot JVM.
var knownGCCollectors = []string{"G1 Young Generation", "G1 Old Generation", "G1 Survivor Space"}

type probeConfig struct {
	JolokiaURL string
	Username   string
	Password   string
	Timeout    time.Duration
	Interval   time.Duration
}

// TomcatProbe collects Apache Tomcat metrics via Jolokia HTTP REST.
type TomcatProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *jolokiaClient
	entitySrc    *types.SimpleEntitySource
	unregister   func()
}

// NewTomcatProbe constructs the probe. Config errors surface here.
func NewTomcatProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.tomcat")

	cfg := probeConfig{
		JolokiaURL: defaultJolokiaURL,
		Timeout:    defaultTimeout,
		Interval:   defaultInterval,
	}

	if v, ok := config["jolokia_url"].(string); ok && v != "" {
		cfg.JolokiaURL = strings.TrimRight(v, "/")
	}
	if v, ok := config["username"].(string); ok {
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

	transport := &http.Transport{}
	httpClient := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
	}

	// Wrap transport with BasicAuth if credentials are configured.
	var roundTripper http.RoundTripper = transport
	if cfg.Username != "" {
		roundTripper = &basicAuthTransport{
			wrapped:  transport,
			username: cfg.Username,
			password: cfg.Password,
		}
		httpClient.Transport = roundTripper
	}

	probe := &TomcatProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &jolokiaClient{
			baseURL: cfg.JolokiaURL,
			http:    httpClient,
		},
	}
	probe.SetProbeType(ProbeType)

	// Entity source — extract server.address and server.port from the Jolokia URL.
	addr, port := jolokiaHostPort(cfg.JolokiaURL)
	entitySrc := types.NewSimpleEntitySource("app.server", map[string]any{
		"server.address":   addr,
		"server.port":      port,
		"app.server.type":  "tomcat",
	})
	probe.entitySrc = entitySrc
	probe.SetEntitySource(entitySrc)

	return probe, nil
}

// jolokiaHostPort parses a Jolokia URL and returns the host and port as
// int64 (the entity ID contract requires int64 for numeric fields).
// Defaults to "localhost" / 8080 when the URL cannot be parsed or the
// port is absent.
func jolokiaHostPort(rawURL string) (string, int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "localhost", 8080
	}
	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	portStr := u.Port()
	if portStr == "" {
		switch u.Scheme {
		case "https":
			return host, 443
		default:
			return host, 80
		}
	}
	var port int64
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return host, 8080
		}
		port = port*10 + int64(c-'0')
	}
	return host, port
}

// basicAuthTransport injects a Basic Authorization header on every request.
type basicAuthTransport struct {
	wrapped  http.RoundTripper
	username string
	password string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
		[]byte(t.username+":"+t.password),
	))
	return t.wrapped.RoundTrip(cloned)
}

func (p *TomcatProbe) ShouldStart() bool          { return true }
func (p *TomcatProbe) GetInterval() time.Duration  { return p.cfg.Interval }

func (p *TomcatProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("jolokia_url", p.cfg.JolokiaURL).
		Msg("Starting tomcat probe")
	p.unregister = entity.RegisterSource(p.EntitySource())
	return nil
}

func (p *TomcatProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.http.CloseIdleConnections()
	return nil
}

// Collect gathers Tomcat / JVM metrics from Jolokia. A Jolokia error
// sets senhub.tomcat.up=0 and returns nil (measurement, not error).
func (p *TomcatProbe) Collect() ([]data_store.DataPoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	now := time.Now()
	var points []data_store.DataPoint
	up := float32(1)

	// Probe reachability: attempt a simple read. If Jolokia is down, mark
	// up=0 and return — there is nothing useful to collect.
	_, err := p.client.readInt64(ctx, "java.lang:type=Threading", "ThreadCount")
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("url", p.cfg.JolokiaURL).Msg("tomcat probe: Jolokia unreachable")
		up = 0
		p.entitySrc.SetUp(false, nil)
		points = append(points, data_store.DataPoint{
			Name:      "senhub.tomcat.up",
			Value:     up,
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "metric_type", Value: "availability"}},
		})
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	p.entitySrc.SetUp(true, nil)
	points = append(points, data_store.DataPoint{
		Name:      "senhub.tomcat.up",
		Value:     up,
		Timestamp: now,
		Tags:      []tags.Tag{{Key: "metric_type", Value: "availability"}},
	})

	// GlobalRequestProcessor metrics (per connector).
	points = append(points, p.collectRequestProcessor(ctx, now)...)

	// ThreadPool metrics (per connector).
	points = append(points, p.collectThreadPool(ctx, now)...)

	// Session metrics (per web application context).
	points = append(points, p.collectSessions(ctx, now)...)

	// JVM Heap memory.
	points = append(points, p.collectHeapMemory(ctx, now)...)

	// JVM GarbageCollector (per collector).
	points = append(points, p.collectGC(ctx, now)...)

	// JVM Threading totals.
	points = append(points, p.collectThreading(ctx, now)...)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// collectRequestProcessor reads Catalina:type=GlobalRequestProcessor,name=<connector>
// for each known HTTP connector. Missing connectors are silently skipped.
func (p *TomcatProbe) collectRequestProcessor(ctx context.Context, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint
	for _, connector := range knownConnectors {
		mbean := fmt.Sprintf("Catalina:type=GlobalRequestProcessor,name=\"%s\"", connector)
		connTag := tags.Tag{Key: "connector", Value: connector}

		reqCount, err := p.client.readInt64(ctx, mbean, "requestCount")
		if err != nil {
			// Connector not present or Jolokia error — skip silently.
			continue
		}
		bytesReceived, _ := p.client.readInt64(ctx, mbean, "bytesReceived")
		bytesSent, _ := p.client.readInt64(ctx, mbean, "bytesSent")
		processingTime, _ := p.client.readInt64(ctx, mbean, "processingTime")
		errorCount, _ := p.client.readInt64(ctx, mbean, "errorCount")

		reqTags := []tags.Tag{connTag, {Key: "metric_type", Value: "requests"}}
		points = append(points,
			data_store.DataPoint{Name: "tomcat.requests.total", Value: float32(reqCount), Timestamp: now, Tags: reqTags},
			data_store.DataPoint{Name: "tomcat.bytes.received", Value: float32(bytesReceived), Timestamp: now, Tags: reqTags},
			data_store.DataPoint{Name: "tomcat.bytes.sent", Value: float32(bytesSent), Timestamp: now, Tags: reqTags},
			data_store.DataPoint{Name: "tomcat.processing_time", Value: float32(processingTime), Timestamp: now, Tags: reqTags},
			data_store.DataPoint{Name: "tomcat.errors.total", Value: float32(errorCount), Timestamp: now, Tags: reqTags},
		)
	}
	return points
}

// collectThreadPool reads Catalina:type=ThreadPool,name=<connector>.
func (p *TomcatProbe) collectThreadPool(ctx context.Context, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint
	for _, connector := range knownConnectors {
		mbean := fmt.Sprintf("Catalina:type=ThreadPool,name=\"%s\"", connector)
		connTag := tags.Tag{Key: "connector", Value: connector}

		current, err := p.client.readInt64(ctx, mbean, "currentThreadCount")
		if err != nil {
			continue
		}
		busy, _ := p.client.readInt64(ctx, mbean, "currentThreadsBusy")
		max, _ := p.client.readInt64(ctx, mbean, "maxThreads")

		threadTags := []tags.Tag{connTag, {Key: "metric_type", Value: "threads"}}
		points = append(points,
			data_store.DataPoint{Name: "tomcat.threads.current", Value: float32(current), Timestamp: now, Tags: threadTags},
			data_store.DataPoint{Name: "tomcat.threads.busy", Value: float32(busy), Timestamp: now, Tags: threadTags},
			data_store.DataPoint{Name: "tomcat.threads.max", Value: float32(max), Timestamp: now, Tags: threadTags},
		)
	}
	return points
}

// collectSessions reads Catalina:type=Manager,host=*,context=* activeSessions.
// Jolokia's bulk-read for wildcard MBeans returns a nested map; we flatten it.
func (p *TomcatProbe) collectSessions(ctx context.Context, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	// Attempt bulk-read for all Manager MBeans. The response value is a map
	// keyed by full MBean name when using a wildcard in the MBean path.
	// If the bulk-read fails we simply skip session metrics.
	raw, err := p.client.readMap(ctx, "Catalina:type=Manager,host=*,context=*", "activeSessions")
	if err != nil {
		p.moduleLogger.Debug().Err(err).Msg("tomcat: could not read session MBeans (no web apps deployed?)")
		return points
	}

	// The value is: { "<MBeanName>": <activeSessions_int> }
	for mbeanKey, val := range raw {
		context := extractContextFromManagerMBean(mbeanKey)
		var sessions float32
		switch v := val.(type) {
		case float64:
			sessions = float32(v)
		case float32:
			sessions = v
		default:
			continue
		}
		sessionTags := []tags.Tag{
			{Key: "context", Value: context},
			{Key: "metric_type", Value: "sessions"},
		}
		points = append(points, data_store.DataPoint{
			Name:      "tomcat.sessions.active",
			Value:     sessions,
			Timestamp: now,
			Tags:      sessionTags,
		})
	}
	return points
}

// extractContextFromManagerMBean extracts the context path from a Catalina Manager MBean name.
// Example: "Catalina:context=/myapp,host=localhost,type=Manager" → "/myapp"
// The MBean name format is "Domain:key1=val1,key2=val2,...". We first strip
// the domain prefix up to the colon, then scan the key=value pairs.
func extractContextFromManagerMBean(mbean string) string {
	// Strip the domain prefix (e.g. "Catalina:").
	kv := mbean
	if idx := strings.IndexByte(mbean, ':'); idx >= 0 {
		kv = mbean[idx+1:]
	}
	for _, part := range strings.Split(kv, ",") {
		if strings.HasPrefix(part, "context=") {
			return strings.TrimPrefix(part, "context=")
		}
	}
	return mbean
}

// collectHeapMemory reads java.lang:type=Memory HeapMemoryUsage composite.
func (p *TomcatProbe) collectHeapMemory(ctx context.Context, now time.Time) []data_store.DataPoint {
	heapMap, err := p.client.readMap(ctx, "java.lang:type=Memory", "HeapMemoryUsage")
	if err != nil {
		p.moduleLogger.Debug().Err(err).Msg("tomcat: could not read HeapMemoryUsage")
		return nil
	}

	memTags := []tags.Tag{{Key: "metric_type", Value: "memory"}}
	var points []data_store.DataPoint
	for _, field := range []struct {
		key  string
		name string
	}{
		{"used", "jvm.memory.heap.used"},
		{"committed", "jvm.memory.heap.committed"},
		{"max", "jvm.memory.heap.max"},
	} {
		v, ok := heapMap[field.key]
		if !ok {
			continue
		}
		var fv float32
		switch num := v.(type) {
		case float64:
			fv = float32(num)
		case float32:
			fv = num
		default:
			continue
		}
		points = append(points, data_store.DataPoint{
			Name:      field.name,
			Value:     fv,
			Timestamp: now,
			Tags:      memTags,
		})
	}
	return points
}

// collectGC reads java.lang:type=GarbageCollector,name=<collector> for known G1 collectors.
func (p *TomcatProbe) collectGC(ctx context.Context, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint
	for _, collector := range knownGCCollectors {
		mbean := fmt.Sprintf("java.lang:type=GarbageCollector,name=\"%s\"", collector)
		collTag := tags.Tag{Key: "collector", Value: collector}

		count, err := p.client.readInt64(ctx, mbean, "CollectionCount")
		if err != nil {
			continue
		}
		elapsed, _ := p.client.readInt64(ctx, mbean, "CollectionTime")

		gcTags := []tags.Tag{collTag, {Key: "metric_type", Value: "gc"}}
		points = append(points,
			data_store.DataPoint{Name: "jvm.gc.collections.count", Value: float32(count), Timestamp: now, Tags: gcTags},
			data_store.DataPoint{Name: "jvm.gc.collections.elapsed", Value: float32(elapsed), Timestamp: now, Tags: gcTags},
		)
	}
	return points
}

// collectThreading reads java.lang:type=Threading thread counts.
func (p *TomcatProbe) collectThreading(ctx context.Context, now time.Time) []data_store.DataPoint {
	threadCount, err := p.client.readInt64(ctx, "java.lang:type=Threading", "ThreadCount")
	if err != nil {
		return nil
	}

	threadTags := []tags.Tag{{Key: "metric_type", Value: "threads"}}
	return []data_store.DataPoint{
		{Name: "jvm.threads.count", Value: float32(threadCount), Timestamp: now, Tags: threadTags},
	}
}
