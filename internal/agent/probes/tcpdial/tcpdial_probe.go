// Package tcpdial implements the free tcp_dial probe: raw TCP connect
// latency to host:port targets (#159). For a NetScaler VIP, a Citrix
// broker, an AD DC or a fileserver, a measured connect() is faster and
// more reliable than an HTTP round trip. Same bounded multi-target
// chassis as icmp_check / http_check.
package tcpdial

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "tcp_dial"

type dialResult struct {
	target   string
	up       bool
	duration time.Duration
	err      error
}

type dialFunc func(ctx context.Context, target string) dialResult

type TCPDialProbe struct {
	*types.BaseProbe
	config       checkConfig
	moduleLogger *logger.ModuleLogger
	dial         dialFunc
	hostID       string
	entitySrc    *tcpdialEntitySource
}

type checkConfig struct {
	Targets  []string
	Timeout  time.Duration
	Interval time.Duration
}

const (
	defaultTimeout     = 5 * time.Second
	defaultInterval    = 60 * time.Second
	maxParallelTargets = 8
)

// NewTCPDialProbe constructs the probe. Config errors surface here.
func NewTCPDialProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.tcp_dial")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	var hostID string
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}

	probe := &TCPDialProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		hostID:       hostID,
	}
	probe.SetProbeType(ProbeType)
	probe.entitySrc = newTCPDialEntitySource(agentstate.GetAgentInstanceID)
	probe.SetEntitySource(probe.entitySrc)
	probe.dial = probe.dialOnce
	return probe, nil
}

func parseConfig(config map[string]interface{}) (checkConfig, error) {
	cfg := checkConfig{Timeout: defaultTimeout, Interval: defaultInterval}

	raw, ok := config["targets"]
	if !ok {
		return cfg, fmt.Errorf("tcp_dial requires a targets list")
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok || s == "" {
				return cfg, fmt.Errorf("tcp_dial targets must be non-empty host:port strings (got %T)", item)
			}
			if _, _, err := net.SplitHostPort(s); err != nil {
				return cfg, fmt.Errorf("tcp_dial target %q is not host:port: %w", s, err)
			}
			cfg.Targets = append(cfg.Targets, s)
		}
	case []string:
		cfg.Targets = v
	default:
		return cfg, fmt.Errorf("tcp_dial targets must be a list (got %T)", raw)
	}
	if len(cfg.Targets) == 0 {
		return cfg, fmt.Errorf("tcp_dial requires at least one target")
	}

	if v, ok := types.IntParam(config, "timeout"); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	return cfg, nil
}

func (p *TCPDialProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *TCPDialProbe) ShouldStart() bool          { return true }
func (p *TCPDialProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *TCPDialProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("targets", p.config.Targets).
		Msg("Starting tcp_dial probe")
	return nil
}

func (p *TCPDialProbe) OnShutdown(ctx context.Context) error { return nil }

// Collect dials every target without a cancellation signal; the poller
// calls CollectContext.
func (p *TCPDialProbe) Collect() ([]data_store.DataPoint, error) {
	return p.CollectContext(context.Background())
}

// CollectContext dials every target (bounded parallelism). A refused or
// timed out target is a measurement (up=0), never a collection error. A
// cycle whose context ends is not: its targets did not fail, so it
// returns ctx.Err() and no datapoint.
func (p *TCPDialProbe) CollectContext(ctx context.Context) ([]data_store.DataPoint, error) {
	now := time.Now()

	infos := make([]targetInfo, len(p.config.Targets))
	for i, target := range p.config.Targets {
		infos[i] = buildTargetInfo(target, p.hostID)
	}
	p.entitySrc.rebuild(p.config.Targets, p.hostID)

	results := make([]dialResult, len(p.config.Targets))

	sem := make(chan struct{}, maxParallelTargets)
	var wg sync.WaitGroup
	for i, target := range p.config.Targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.dial(ctx, target)
		}(i, target)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tcp_dial collection interrupted: %w", err)
	}

	var points []data_store.DataPoint
	for i, res := range results {
		points = append(points, p.buildDatapoints(res, now, infos[i])...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *TCPDialProbe) buildDatapoints(res dialResult, ts time.Time, info targetInfo) []data_store.DataPoint {
	identityAndTarget := make([]tags.Tag, 0, len(info.identityTags)+1)
	identityAndTarget = append(identityAndTarget, tags.Tag{Key: "target", Value: res.target})
	identityAndTarget = append(identityAndTarget, info.identityTags...)

	availabilityTags := make([]tags.Tag, 0, len(identityAndTarget)+1)
	availabilityTags = append(availabilityTags, identityAndTarget...)
	availabilityTags = append(availabilityTags, tags.Tag{Key: "metric_type", Value: "availability"})

	statusTags := make([]tags.Tag, 0, len(identityAndTarget)+1)
	statusTags = append(statusTags, identityAndTarget...)
	statusTags = append(statusTags, tags.Tag{Key: "metric_type", Value: "status"})

	up := float64(0)
	if res.up {
		up = 1
	}

	checkResult := dbcommon.CheckOK
	if res.err != nil {
		checkResult = dbcommon.ClassifyError(res.err)
	}
	statusValue := float64(checkResult)

	if res.err != nil {
		p.moduleLogger.Warn().
			Err(res.err).
			Str("target", res.target).
			Msg("tcp_dial target failed")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.tcpdial.up", Value: up, Timestamp: ts, Tags: availabilityTags},
		{Name: dbcommon.CheckResultMetric, Value: statusValue, Timestamp: ts, Tags: statusTags},
	}
	if res.up {
		points = append(points,
			data_store.DataPoint{Name: "senhub.tcpdial.duration", Value: res.duration.Seconds() * 1000, Timestamp: ts, Tags: availabilityTags},
		)
	}

	if res.err != nil && info.local &&
		checkResult == dbcommon.CheckTargetDown &&
		dbcommon.IsConnectionRefused(res.err) {
		diagnosticTags := make([]tags.Tag, 0, len(identityAndTarget)+1)
		diagnosticTags = append(diagnosticTags, identityAndTarget...)
		diagnosticTags = append(diagnosticTags, tags.Tag{Key: "reason", Value: dbcommon.UnresolvedReasonNoListener})

		points = append(points, data_store.DataPoint{
			Name:      dbcommon.UnresolvedTargetMetric,
			Value:     1,
			Timestamp: ts,
			Tags:      diagnosticTags,
		})
	}

	return points
}

// dialOnce is the production dialFunc: one measured TCP connect.
func (p *TCPDialProbe) dialOnce(ctx context.Context, target string) dialResult {
	res := dialResult{target: target}
	start := time.Now()
	dialer := net.Dialer{Timeout: p.config.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	res.duration = time.Since(start)
	if err != nil {
		res.err = fmt.Errorf("dialing %s: %w", target, err)
		return res
	}
	_ = conn.Close()
	res.up = true
	return res
}
