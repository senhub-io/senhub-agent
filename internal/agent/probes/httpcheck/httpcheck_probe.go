// Package httpcheck implements the free http_check probe: multi-target
// HTTP(S) checks with status validation, latency phase breakdown
// (DNS / connect / TLS / TTFB / total), response size, optional content
// matching, and TLS certificate expiry as a first-class metric — the
// senhub stack's first TLS-expiry signal (#300).
//
// Naming: aligned with the otelcol-contrib httpcheck receiver where it
// has a metric (httpcheck.duration); extended under senhub.httpcheck.*
// for what contrib lacks (phases, TLS expiry, size, content match).
//
// Shares the bounded multi-target fan-out chassis with icmp_check.
package httpcheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "http_check"

// httpResult is the per-target outcome the datapoint builder consumes.
type httpResult struct {
	target        string
	statusCode    int
	up            bool
	dnsTime       time.Duration
	connectTime   time.Duration
	tlsTime       time.Duration
	ttfb          time.Duration
	totalTime     time.Duration
	responseBytes int64
	tlsDaysLeft   float64 // negative when expired; NaN-free: -1e9 marks "no TLS"
	tlsCertValid  float64 // 1 = valid, 0 = expired; -1e9 marks "no TLS"
	tlsIssuer     string
	tlsSubject    string
	hasTLS        bool
	contentMatch  *bool // nil when no matcher configured
	err           error
}

type checkFunc func(target string) httpResult

type HTTPCheckProbe struct {
	*types.BaseProbe
	config       checkConfig
	moduleLogger *logger.ModuleLogger
	check        checkFunc
	client       *http.Client
	contentRe    *regexp.Regexp
}

type checkConfig struct {
	Targets            []string
	Method             string
	Timeout            time.Duration
	Interval           time.Duration
	ExpectedStatus     int // 0 = any 2xx/3xx
	ContentMatch       string
	InsecureSkipVerify bool
	MaxBodyBytes       int64
}

const (
	defaultTimeout      = 10 * time.Second
	defaultInterval     = 60 * time.Second
	defaultMaxBodyBytes = 1 << 20 // read at most 1 MiB for size/content
	maxParallelTargets  = 8
	noTLSSentinel       = -1e9
)

// NewHTTPCheckProbe constructs the probe. Config errors surface here.
func NewHTTPCheckProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.http_check")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	var contentRe *regexp.Regexp
	if cfg.ContentMatch != "" {
		contentRe, err = regexp.Compile(cfg.ContentMatch)
		if err != nil {
			return nil, fmt.Errorf("http_check content_match is not a valid regexp: %w", err)
		}
	}

	probe := &HTTPCheckProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		contentRe:    contentRe,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: cfg.InsecureSkipVerify, // #nosec G402 - operator opt-in for self-signed labs
				},
				DisableKeepAlives: true, // each cycle measures a full fresh handshake
			},
			// Checks measure the target, not its redirect chain.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	probe.SetProbeType(ProbeType)
	probe.check = probe.checkOnce
	return probe, nil
}

func parseConfig(config map[string]interface{}) (checkConfig, error) {
	cfg := checkConfig{
		Method:       http.MethodGet,
		Timeout:      defaultTimeout,
		Interval:     defaultInterval,
		MaxBodyBytes: defaultMaxBodyBytes,
	}

	raw, ok := config["targets"]
	if !ok {
		return cfg, fmt.Errorf("http_check requires a targets list")
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok || s == "" {
				return cfg, fmt.Errorf("http_check targets must be non-empty URL strings (got %T)", item)
			}
			cfg.Targets = append(cfg.Targets, s)
		}
	case []string:
		cfg.Targets = v
	default:
		return cfg, fmt.Errorf("http_check targets must be a list (got %T)", raw)
	}
	if len(cfg.Targets) == 0 {
		return cfg, fmt.Errorf("http_check requires at least one target")
	}

	if v, ok := config["method"].(string); ok && v != "" {
		cfg.Method = v
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["expected_status"].(int); ok && v > 0 {
		cfg.ExpectedStatus = v
	}
	if v, ok := config["content_match"].(string); ok {
		cfg.ContentMatch = v
	}
	if v, ok := config["insecure_skip_verify"].(bool); ok {
		cfg.InsecureSkipVerify = v
	}
	return cfg, nil
}

func (p *HTTPCheckProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *HTTPCheckProbe) ShouldStart() bool          { return true }
func (p *HTTPCheckProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *HTTPCheckProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("targets", p.config.Targets).
		Str("method", p.config.Method).
		Msg("Starting http_check probe")
	return nil
}

func (p *HTTPCheckProbe) OnShutdown(ctx context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect checks every target (bounded parallelism). A failing target
// is a measurement (up=0), never a collection error.
func (p *HTTPCheckProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	results := make([]httpResult, len(p.config.Targets))

	sem := make(chan struct{}, maxParallelTargets)
	var wg sync.WaitGroup
	for i, target := range p.config.Targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.check(target)
		}(i, target)
	}
	wg.Wait()

	var points []data_store.DataPoint
	for _, res := range results {
		points = append(points, p.buildDatapoints(res, now)...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// buildDatapoints converts one check outcome into the metric set.
// Durations are milliseconds on the wire (OTel seconds via the YAML
// value_scale); TLS expiry is days remaining (negative once expired).
func (p *HTTPCheckProbe) buildDatapoints(res httpResult, ts time.Time) []data_store.DataPoint {
	baseTags := []tags.Tag{
		{Key: "target", Value: res.target},
		{Key: "metric_type", Value: "availability"},
	}

	up := float32(0)
	if res.up {
		up = 1
	}
	if res.err != nil {
		p.moduleLogger.Warn().
			Err(res.err).
			Str("target", res.target).
			Msg("http_check target failed")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.httpcheck.up", Value: up, Timestamp: ts, Tags: baseTags},
	}
	if res.statusCode > 0 {
		points = append(points,
			data_store.DataPoint{Name: "senhub.httpcheck.status.code", Value: float32(res.statusCode), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "httpcheck.duration", Value: float32(res.totalTime.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "senhub.httpcheck.duration.dns", Value: float32(res.dnsTime.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "senhub.httpcheck.duration.connect", Value: float32(res.connectTime.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "senhub.httpcheck.duration.ttfb", Value: float32(res.ttfb.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "senhub.httpcheck.response.size", Value: float32(res.responseBytes), Timestamp: ts, Tags: baseTags},
		)
		if res.hasTLS {
			points = append(points,
				data_store.DataPoint{Name: "senhub.httpcheck.duration.tls", Value: float32(res.tlsTime.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			)
		}
	}
	if res.hasTLS && res.tlsDaysLeft > noTLSSentinel {
		tlsTags := append(baseTags,
			tags.Tag{Key: "tls.issuer", Value: res.tlsIssuer},
			tags.Tag{Key: "tls.subject", Value: res.tlsSubject},
		)
		points = append(points,
			data_store.DataPoint{Name: "senhub.httpcheck.tls.expiry", Value: float32(res.tlsDaysLeft), Timestamp: ts, Tags: tlsTags},
		)
		if res.tlsCertValid > noTLSSentinel {
			points = append(points,
				data_store.DataPoint{Name: "senhub.httpcheck.tls.valid", Value: float32(res.tlsCertValid), Timestamp: ts, Tags: tlsTags},
			)
		}
	}
	if res.contentMatch != nil {
		match := float32(0)
		if *res.contentMatch {
			match = 1
		}
		points = append(points,
			data_store.DataPoint{Name: "senhub.httpcheck.content.match", Value: match, Timestamp: ts, Tags: baseTags},
		)
	}
	return points
}

// checkOnce is the production checkFunc: one traced HTTP request.
func (p *HTTPCheckProbe) checkOnce(target string) httpResult {
	res := httpResult{target: target, tlsDaysLeft: noTLSSentinel, tlsCertValid: noTLSSentinel}

	var dnsStart, connectStart, tlsStart, reqStart time.Time
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:           func(httptrace.DNSDoneInfo) { res.dnsTime = time.Since(dnsStart) },
		ConnectStart:      func(string, string) { connectStart = time.Now() },
		ConnectDone:       func(string, string, error) { res.connectTime = time.Since(connectStart) },
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			res.tlsTime = time.Since(tlsStart)
			if err == nil {
				res.hasTLS = true
				if len(state.PeerCertificates) > 0 {
					cert := state.PeerCertificates[0]
					res.tlsDaysLeft = time.Until(cert.NotAfter).Hours() / 24
					if time.Now().After(cert.NotAfter) {
						res.tlsCertValid = 0
					} else {
						res.tlsCertValid = 1
					}
					res.tlsIssuer = cert.Issuer.CommonName
					res.tlsSubject = cert.Subject.CommonName
				}
			}
		},
		GotFirstResponseByte: func() { res.ttfb = time.Since(reqStart) },
	}

	req, err := http.NewRequest(p.config.Method, target, nil)
	if err != nil {
		res.err = fmt.Errorf("building request for %s: %w", target, err)
		return res
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	reqStart = time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		res.err = fmt.Errorf("checking %s: %w", target, err)
		return res
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, p.config.MaxBodyBytes))
	res.totalTime = time.Since(reqStart)
	res.statusCode = resp.StatusCode
	res.responseBytes = int64(len(body))

	if p.config.ExpectedStatus > 0 {
		res.up = resp.StatusCode == p.config.ExpectedStatus
	} else {
		res.up = resp.StatusCode >= 200 && resp.StatusCode < 400
	}

	if p.contentRe != nil {
		match := p.contentRe.Match(body)
		res.contentMatch = &match
		if !match {
			res.up = false
		}
	}
	return res
}
