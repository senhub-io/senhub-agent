// Package dnslatency implements the free dns_latency probe: DNS
// resolution latency for a set of names, optionally against explicit
// resolvers (#158). Slow DNS is a frequent cause of perceived
// workstation/VDA slowness (logon, app launch, share access); this
// makes it a first-class measurement. Same bounded multi-target
// chassis as the other active checks.
package dnslatency

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "dns_latency"

// systemResolverLabel tags series resolved through the OS resolver
// (no explicit resolvers configured).
const systemResolverLabel = "system"

type lookupResult struct {
	name     string
	resolver string // systemResolverLabel or "ip:port"
	up       bool
	duration time.Duration
	answers  int
	err      error
}

type lookupFunc func(name, resolver string) lookupResult

type DNSLatencyProbe struct {
	*types.BaseProbe
	config       checkConfig
	moduleLogger *logger.ModuleLogger
	lookup       lookupFunc
}

type checkConfig struct {
	Names     []string
	Resolvers []string // empty = system resolver
	Timeout   time.Duration
	Interval  time.Duration
}

const (
	defaultTimeout     = 5 * time.Second
	defaultInterval    = 60 * time.Second
	maxParallelLookups = 8
)

// NewDNSLatencyProbe constructs the probe. Config errors surface here.
func NewDNSLatencyProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.dns_latency")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &DNSLatencyProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
	}
	probe.SetProbeType(ProbeType)
	probe.lookup = probe.lookupOnce
	return probe, nil
}

func parseConfig(config map[string]interface{}) (checkConfig, error) {
	cfg := checkConfig{Timeout: defaultTimeout, Interval: defaultInterval}

	raw, ok := config["names"]
	if !ok {
		return cfg, fmt.Errorf("dns_latency requires a names list")
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok || s == "" {
				return cfg, fmt.Errorf("dns_latency names must be non-empty strings (got %T)", item)
			}
			cfg.Names = append(cfg.Names, s)
		}
	case []string:
		cfg.Names = v
	default:
		return cfg, fmt.Errorf("dns_latency names must be a list (got %T)", raw)
	}
	if len(cfg.Names) == 0 {
		return cfg, fmt.Errorf("dns_latency requires at least one name")
	}

	if raw, ok := config["resolvers"]; ok {
		list, ok := raw.([]interface{})
		if !ok {
			return cfg, fmt.Errorf("dns_latency resolvers must be a list (got %T)", raw)
		}
		for _, item := range list {
			s, ok := item.(string)
			if !ok || s == "" {
				return cfg, fmt.Errorf("dns_latency resolvers must be non-empty ip:port strings (got %T)", item)
			}
			if _, _, err := net.SplitHostPort(s); err != nil {
				// Bare IP: default DNS port.
				s = net.JoinHostPort(s, "53")
			}
			cfg.Resolvers = append(cfg.Resolvers, s)
		}
	}

	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	return cfg, nil
}

func (p *DNSLatencyProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *DNSLatencyProbe) ShouldStart() bool          { return true }
func (p *DNSLatencyProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *DNSLatencyProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("names", p.config.Names).
		Strs("resolvers", p.config.Resolvers).
		Msg("Starting dns_latency probe")
	return nil
}

func (p *DNSLatencyProbe) OnShutdown(ctx context.Context) error { return nil }

// Collect resolves every (name, resolver) pair with bounded
// parallelism. A failing lookup is a measurement (up=0), never a
// collection error.
func (p *DNSLatencyProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	resolvers := p.config.Resolvers
	if len(resolvers) == 0 {
		resolvers = []string{systemResolverLabel}
	}

	type pair struct{ name, resolver string }
	var pairs []pair
	for _, name := range p.config.Names {
		for _, resolver := range resolvers {
			pairs = append(pairs, pair{name, resolver})
		}
	}

	results := make([]lookupResult, len(pairs))
	sem := make(chan struct{}, maxParallelLookups)
	var wg sync.WaitGroup
	for i, pr := range pairs {
		wg.Add(1)
		go func(i int, pr pair) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.lookup(pr.name, pr.resolver)
		}(i, pr)
	}
	wg.Wait()

	var points []data_store.DataPoint
	for _, res := range results {
		points = append(points, p.buildDatapoints(res, now)...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *DNSLatencyProbe) buildDatapoints(res lookupResult, ts time.Time) []data_store.DataPoint {
	baseTags := []tags.Tag{
		{Key: "name", Value: res.name},
		{Key: "resolver", Value: res.resolver},
		{Key: "metric_type", Value: "availability"},
	}
	up := float32(0)
	if res.up {
		up = 1
	}
	if res.err != nil {
		p.moduleLogger.Warn().
			Err(res.err).
			Str("name", res.name).
			Str("resolver", res.resolver).
			Msg("dns_latency lookup failed")
	}
	points := []data_store.DataPoint{
		{Name: "senhub.dns.up", Value: up, Timestamp: ts, Tags: baseTags},
	}
	if res.up {
		points = append(points,
			data_store.DataPoint{Name: "senhub.dns.lookup.duration", Value: float32(res.duration.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "senhub.dns.answers", Value: float32(res.answers), Timestamp: ts, Tags: baseTags},
		)
	}
	return points
}

// lookupOnce is the production lookupFunc: one measured resolution.
func (p *DNSLatencyProbe) lookupOnce(name, resolver string) lookupResult {
	res := lookupResult{name: name, resolver: resolver}

	r := net.DefaultResolver
	if resolver != systemResolverLabel {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: p.config.Timeout}
				return d.DialContext(ctx, network, resolver)
			},
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.config.Timeout)
	defer cancel()

	start := time.Now()
	addrs, err := r.LookupHost(ctx, name)
	res.duration = time.Since(start)
	if err != nil {
		res.err = fmt.Errorf("resolving %s via %s: %w", name, resolver, err)
		return res
	}
	res.up = true
	res.answers = len(addrs)
	return res
}
