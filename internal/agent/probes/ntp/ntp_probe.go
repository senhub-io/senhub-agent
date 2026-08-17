// Package ntp implements the free ntp probe: it measures the local clock's
// error directly, by exchanging NTP packets with time servers the operator
// names.
//
// It is deliberately independent of any local time daemon. chrony, ntpd,
// systemd-timesyncd and the Windows Time service each report their own view of
// synchronisation, in their own format, through their own tool — and a host
// running none of them reports nothing at all. This probe asks the same
// question on every platform: does this machine's clock agree with a reference,
// and by how much.
//
// It answers a different question from the chrony probe rather than replacing
// it. chrony reports what the daemon believes about the clock it steers; this
// reports what an independent server observes. Running both is how a daemon
// that is confidently synchronised to a wrong source becomes visible: the two
// offsets disagree.
//
// Accuracy: good enough to see a clock that is wrong by more than a few tens of
// milliseconds, which is the scale at which Kerberos, TLS validity, log
// correlation and one-time passwords break. It is NOT a precision measurement —
// the limit is documented on decode() and best() in client.go, and surfaced to
// the reader as ntp.round_trip.delay next to every offset.
package ntp

import (
	"context"
	"errors"
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
const ProbeType = "ntp"

const (
	defaultTimeout    = 5 * time.Second
	defaultSamples    = 4
	maxSamples        = 16
	maxParallelChecks = 8

	// defaultInterval is deliberately far longer than a collection probe's.
	// Clock error moves slowly, and a query is traffic sent to somebody else's
	// server — a fleet polling a public pool every 30 s is abuse of it. Five
	// minutes still catches a filtered UDP 123 or a stopped daemon well before
	// anything downstream notices.
	defaultInterval = 300 * time.Second
)

// Why the reason is its own one-hot series, as on the chrony probe: an operator
// reported that "no time daemon here" and "the probe cannot reach the server"
// produced the identical observable, a probe emitting nothing but up=0. Only
// one of those is something to fix, and telling them apart needed the agent
// log. A reason is a string, a string cannot be a metric value, and a dashboard
// should be able to count hosts in a state without decoding a number.
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

// serverResult is the outcome of one cycle against one server.
type serverResult struct {
	server string
	sample sample
	err    error
}

type queryFunc func(server string, timeout time.Duration) (sample, error)

// NTPProbe measures the local clock against one or more NTP servers.
type NTPProbe struct {
	*types.BaseProbe
	config       checkConfig
	moduleLogger *logger.ModuleLogger
	query        queryFunc
}

// NewNTPProbe constructs the probe. Config errors surface here.
func NewNTPProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.ntp")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &NTPProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		query:        query,
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

// parseConfig validates the probe block.
//
// servers has no default on purpose. A default would point every agent that
// ever enables this probe at somebody else's infrastructure, and it would also
// be the wrong measurement: the useful comparison is against the reference the
// host is supposed to be following, which only the operator knows.
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

	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["samples"].(int); ok && v > 0 {
		if v > maxSamples {
			return cfg, fmt.Errorf("ntp samples is %d, which exceeds the maximum of %d — more packets per cycle stop improving the estimate and start looking like abuse to the server", v, maxSamples)
		}
		cfg.Samples = v
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
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

// Collect measures the clock against every configured server with bounded
// parallelism. A server that fails is a measurement (up=0 plus the reason),
// never a collection error — one unreachable time source must not suppress the
// reading taken from another.
func (p *NTPProbe) Collect() ([]data_store.DataPoint, error) {
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
			results[i] = p.measure(server)
		}(i, server)
	}
	wg.Wait()

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
func (p *NTPProbe) measure(server string) serverResult {
	var samples []sample
	var lastErr error
	for i := 0; i < p.config.Samples; i++ {
		s, err := p.query(server, p.config.Timeout)
		if err != nil {
			lastErr = err
			// A refusal applies to us, not to this packet. Retrying is the
			// behaviour the server is asking us to stop.
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
	baseTags := []tags.Tag{
		{Key: "server", Value: res.server},
		{Key: "metric_type", Value: "time_sync"},
	}

	up := float64(1)
	if res.err != nil {
		up = 0
		p.moduleLogger.Warn().
			Err(res.err).
			Str("server", res.server).
			Msg("ntp measurement failed")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.ntp.up", Value: up, Timestamp: ts, Tags: baseTags},
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
