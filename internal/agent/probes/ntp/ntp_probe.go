package ntp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/subcollect"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "ntp"

const (
	defaultTimeout    = 5 * time.Second
	defaultSamples    = 4
	maxSamples        = 16
	maxParallelChecks = 8

	defaultInterval = 300 * time.Second
)

const (
	reasonOK              = "ok"
	reasonUnreachable     = "unreachable"
	reasonRefused         = "refused"
	reasonUnsynchronised  = "unsynchronised"
	reasonInvalidResponse = "invalid_response"
)

var allReasons = []string{
	reasonOK, reasonUnreachable, reasonRefused,
	reasonUnsynchronised, reasonInvalidResponse,
}

type checkConfig struct {
	Servers  []string
	Timeout  time.Duration
	Samples  int
	Interval time.Duration
}

type serverResult struct {
	server string
	sample sample
	err    error
}

type queryFunc func(ctx context.Context, server string, timeout time.Duration) (sample, error)

// NTPProbe measures the local clock against one or more NTP servers.
type NTPProbe struct {
	*types.BaseProbe
	config       checkConfig
	moduleLogger *logger.ModuleLogger
	query        queryFunc
	entitySrc    *ntpEntitySource
	failures     subcollect.Tracker
}

// NewNTPProbe constructs the probe. Config errors surface here.
func NewNTPProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.ntp")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	var hostID string
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}
	entitySrc := newNTPEntitySource(hostID)

	probe := &NTPProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		query:        query,
		entitySrc:    entitySrc,
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySrc)
	return probe, nil
}

// parseConfig validates the probe block.
func parseConfig(config map[string]interface{}) (checkConfig, error) {
	cfg := checkConfig{
		Timeout:  defaultTimeout,
		Samples:  defaultSamples,
		Interval: defaultInterval,
	}

	raw, ok := config["servers"]
	if !ok {
		return cfg, errors.New("ntp requires a servers list naming the NTP server(s) to measure against")
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok || s == "" {
				return cfg, fmt.Errorf("ntp servers must be non-empty host or host:port strings (got %T)", item)
			}
			cfg.Servers = append(cfg.Servers, s)
		}
	case []string:
		cfg.Servers = v
	default:
		return cfg, fmt.Errorf("ntp servers must be a list (got %T)", raw)
	}
	if len(cfg.Servers) == 0 {
		return cfg, errors.New("ntp requires at least one server")
	}
	for _, s := range cfg.Servers {
		if host, _, err := net.SplitHostPort(s); err == nil && host == "" {
			return cfg, fmt.Errorf("ntp server %q has no host part", s)
		}
	}

	if v, ok := types.IntParam(config, "timeout"); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := types.IntParam(config, "samples"); ok && v > 0 {
		if v > maxSamples {
			return cfg, fmt.Errorf("ntp samples is %d, which exceeds the maximum of %d — more packets per cycle stop improving the estimate and start looking like abuse to the server", v, maxSamples)
		}
		cfg.Samples = v
	}
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	return cfg, nil
}

func (p *NTPProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *NTPProbe) ShouldStart() bool          { return true }
func (p *NTPProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *NTPProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Strs("servers", p.config.Servers).
		Int("samples", p.config.Samples).
		Dur("interval", p.config.Interval).
		Msg("Starting ntp probe")
	return nil
}

func (p *NTPProbe) OnShutdown(_ context.Context) error { return nil }

// Collect collects without a cancellation signal; the poller calls
// CollectContext.
func (p *NTPProbe) Collect() ([]data_store.DataPoint, error) {
	return p.CollectContext(context.Background())
}

// CollectContext measures the clock against every configured server with bounded
// parallelism. A server that fails is a measurement (up=0 plus the reason),
// never a collection error — one unreachable time source must not suppress the
// reading taken from another.
func (p *NTPProbe) CollectContext(ctx context.Context) ([]data_store.DataPoint, error) {
	now := time.Now()

	results := make([]serverResult, len(p.config.Servers))
	sem := make(chan struct{}, maxParallelChecks)
	var wg sync.WaitGroup
	for i, server := range p.config.Servers {
		wg.Add(1)
		go func(i int, server string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.measure(ctx, server)
		}(i, server)
	}
	wg.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("ntp collection interrupted: %w", ctxErr)
	}
	// Rebuild the entity snapshot from the configured targets. The targets do
	// not change at runtime; rebuilding each cycle keeps Observe() consistent.
	p.entitySrc.rebuild(p.config.Servers)

	var points []data_store.DataPoint
	for _, res := range results {
		points = append(points, p.buildDatapoints(res, now)...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// measure runs the configured number of exchanges against one server and keeps
// the least-delayed one. Samples are sequential: they exist to catch a
// transient queue, and firing them together would share the queue they are
// meant to sample around.
func (p *NTPProbe) measure(ctx context.Context, server string) serverResult {
	var samples []sample
	var lastErr error
	for i := 0; i < p.config.Samples; i++ {
		s, err := p.query(ctx, server, p.config.Timeout)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			if errors.Is(err, errRefused) {
				break
			}
			continue
		}
		samples = append(samples, s)
	}
	if len(samples) == 0 {
		return serverResult{server: server, err: lastErr}
	}
	return serverResult{server: server, sample: best(samples)}
}

// reasonOf maps a measurement failure to its one-hot state.
func reasonOf(err error) string {
	switch {
	case err == nil:
		return reasonOK
	case errors.Is(err, errUnreachable):
		return reasonUnreachable
	case errors.Is(err, errRefused):
		return reasonRefused
	case errors.Is(err, errUnsynchronised):
		return reasonUnsynchronised
	default:
		return reasonInvalidResponse
	}
}

func (p *NTPProbe) buildDatapoints(res serverResult, ts time.Time) []data_store.DataPoint {
	targetIdentityTags := p.entitySrc.identityTags(res.server)

	baseTags := []tags.Tag{
		{Key: "server", Value: res.server},
		{Key: "metric_type", Value: "time_sync"},
	}
	baseTags = append(baseTags, targetIdentityTags...)

	statusTags := []tags.Tag{
		{Key: "server", Value: res.server},
		{Key: "metric_type", Value: "status"},
	}
	statusTags = append(statusTags, targetIdentityTags...)

	up := float64(1)
	if res.err != nil {
		up = 0
		p.failures.Failed(p.moduleLogger, subcollect.Warn, res.server, res.err, "ntp measurement failed")
	} else {
		p.failures.Succeeded(p.moduleLogger, res.server, "ntp measurement recovered")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.ntp.up", Value: up, Timestamp: ts, Tags: baseTags},
		{Name: dbcommon.CheckResultMetric, Value: float64(dbcommon.ClassifyError(res.err)), Timestamp: ts, Tags: statusTags},
	}

	reason := reasonOf(res.err)
	for _, r := range allReasons {
		v := float64(0)
		if r == reason {
			v = 1
		}
		t := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "reason", Value: r})
		points = append(points, data_store.DataPoint{
			Name: "senhub.ntp.state", Value: v, Timestamp: ts, Tags: t,
		})
	}

	if res.err != nil {
		return points
	}

	s := res.sample
	points = append(points,
		data_store.DataPoint{Name: "ntp.time.offset", Value: durationMS(s.offset), Timestamp: ts, Tags: baseTags},
		data_store.DataPoint{Name: "ntp.round_trip.delay", Value: durationMS(s.roundTrip), Timestamp: ts, Tags: baseTags},
		data_store.DataPoint{Name: "ntp.stratum", Value: float64(s.stratum), Timestamp: ts, Tags: baseTags},
		data_store.DataPoint{Name: "ntp.root.delay", Value: durationMS(s.rootDelay), Timestamp: ts, Tags: baseTags},
		data_store.DataPoint{Name: "ntp.root.dispersion", Value: durationMS(s.rootDispersion), Timestamp: ts, Tags: baseTags},
		data_store.DataPoint{Name: "ntp.leap_status", Value: float64(s.leap), Timestamp: ts, Tags: baseTags},
	)
	return points
}

// durationMS converts to milliseconds, the unit every time value travels in on
// the wire; the transformer scales it back to seconds for OTel.
func durationMS(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
