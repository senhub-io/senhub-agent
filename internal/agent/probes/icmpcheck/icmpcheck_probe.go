// Package icmpcheck implements the free icmp_check probe: multi-target
// ICMP ping with RTT statistics, packet loss and reachability —
// PRTG's most-deployed sensor class, served from the free tier as part
// of the open-core wedge (#299).
//
// The chassis (multi-target fan-out + per-target active check turned
// into datapoints) is deliberately reusable: tcp_dial (#159) and
// dns_latency (#158) are planned on the same shape.
//
// Privileged vs unprivileged: raw ICMP sockets need root/admin;
// unprivileged mode uses ICMP datagram sockets (always available on
// darwin; on Linux gated by the net.ipv4.ping_group_range sysctl).
// Windows requires privileged mode — the probe defaults accordingly
// and the mode is overridable in config.
package icmpcheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier (license claims,
// transformer file name, discriminant registry key).
const ProbeType = "icmp_check"

// pingResult is the per-target outcome the datapoint builder consumes.
// Decoupled from pro-bing so tests inject deterministic results.
type pingResult struct {
	target     string
	resolvedIP string
	sent       int
	received   int
	lossRatio  float64 // 0..1
	minRTT     time.Duration
	avgRTT     time.Duration
	maxRTT     time.Duration
	stddevRTT  time.Duration
	err        error
}

// pingFunc runs one ping round against a target. The production
// implementation wraps pro-bing; tests substitute their own.
type pingFunc func(target string) pingResult

type ICMPCheckProbe struct {
	*types.BaseProbe
	config       checkConfig
	moduleLogger *logger.ModuleLogger
	ping         pingFunc
}

type checkConfig struct {
	Targets    []string
	Count      int
	Timeout    time.Duration
	Interval   time.Duration
	Privileged bool
	PacketSize int
}

const (
	defaultCount       = 4
	defaultTimeout     = 5 * time.Second
	defaultInterval    = 60 * time.Second
	defaultPacketSize  = 56
	maxParallelTargets = 8
)

// NewICMPCheckProbe constructs the probe. Config errors surface here.
func NewICMPCheckProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.icmp_check")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &ICMPCheckProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
	}
	probe.SetProbeType(ProbeType)
	probe.ping = probe.pingOnce
	return probe, nil
}

// defaultPrivileged picks the ping socket mode when the config does not
// say. Windows has no unprivileged ICMP datagram sockets. On Linux,
// datagram ICMP is gated by net.ipv4.ping_group_range, which stock
// Ubuntu/Debian servers ship DISABLED ("1 0") — even root gets
// permission denied on SOCK_DGRAM ICMP (#357, found on sha901). Root
// installs (`install --user root`) therefore get privileged raw
// sockets, the mode that just works. Under the default hardened unit
// (#223, #280) the daemon is non-root and stays in unprivileged mode;
// raw sockets need a CAP_NET_RAW drop-in or a widened
// ping_group_range — see docs/admin-guide/LEAST-PRIVILEGE.md.
func defaultPrivileged(goos string, euid int) bool {
	if goos == "windows" {
		return true
	}
	if goos == "linux" && euid == 0 {
		return true
	}
	return false
}

func parseConfig(config map[string]interface{}) (checkConfig, error) {
	cfg := checkConfig{
		Count:      defaultCount,
		Timeout:    defaultTimeout,
		Interval:   defaultInterval,
		PacketSize: defaultPacketSize,
		Privileged: defaultPrivileged(runtime.GOOS, os.Geteuid()),
	}

	raw, ok := config["targets"]
	if !ok {
		return cfg, fmt.Errorf("icmp_check requires a targets list")
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok || s == "" {
				return cfg, fmt.Errorf("icmp_check targets must be non-empty strings (got %T)", item)
			}
			cfg.Targets = append(cfg.Targets, s)
		}
	case []string:
		cfg.Targets = v
	default:
		return cfg, fmt.Errorf("icmp_check targets must be a list (got %T)", raw)
	}
	if len(cfg.Targets) == 0 {
		return cfg, fmt.Errorf("icmp_check requires at least one target")
	}

	if v, ok := config["count"].(int); ok && v > 0 {
		cfg.Count = v
	}
	if v, ok := config["packet_size"].(int); ok && v > 0 {
		cfg.PacketSize = v
	}
	if v, ok := config["privileged"].(bool); ok {
		cfg.Privileged = v
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	return cfg, nil
}

func (p *ICMPCheckProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *ICMPCheckProbe) ShouldStart() bool          { return true }
func (p *ICMPCheckProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *ICMPCheckProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("targets", p.config.Targets).
		Int("count", p.config.Count).
		Bool("privileged", p.config.Privileged).
		Msg("Starting icmp_check probe")
	return nil
}

func (p *ICMPCheckProbe) OnShutdown(ctx context.Context) error { return nil }

// Collect pings every configured target (bounded parallelism) and
// turns each outcome into datapoints. An unreachable or failing target
// is a measurement (up=0), never a collection error: the probe's job
// is to report unreachability, not to be unhealthy because of it.
func (p *ICMPCheckProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	results := make([]pingResult, len(p.config.Targets))

	sem := make(chan struct{}, maxParallelTargets)
	var wg sync.WaitGroup
	for i, target := range p.config.Targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.ping(target)
		}(i, target)
	}
	wg.Wait()

	var points []data_store.DataPoint
	for _, res := range results {
		points = append(points, p.buildDatapoints(res, now)...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// buildDatapoints converts one ping outcome into the metric set. RTTs
// are emitted in milliseconds (the operator-facing unit); the OTel
// mapping in icmp_check.yaml converts to seconds.
func (p *ICMPCheckProbe) buildDatapoints(res pingResult, ts time.Time) []data_store.DataPoint {
	baseTags := []tags.Tag{
		{Key: "target", Value: res.target},
		{Key: "metric_type", Value: "availability"},
	}
	if res.resolvedIP != "" {
		baseTags = append(baseTags, tags.Tag{Key: "ip", Value: res.resolvedIP})
	}

	up := float32(0)
	if res.err == nil && res.received > 0 {
		up = 1
	}
	if res.err != nil {
		p.moduleLogger.Warn().
			Err(res.err).
			Str("target", res.target).
			Msg("icmp_check target failed")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.icmp.up", Value: up, Timestamp: ts, Tags: baseTags},
		{Name: "senhub.icmp.packet_loss", Value: float32(res.lossRatio * 100), Timestamp: ts, Tags: baseTags},
		{Name: "senhub.icmp.packets.sent", Value: float32(res.sent), Timestamp: ts, Tags: baseTags},
		{Name: "senhub.icmp.packets.received", Value: float32(res.received), Timestamp: ts, Tags: baseTags},
	}
	// RTT statistics only make sense when at least one reply came back.
	if res.received > 0 {
		points = append(points,
			data_store.DataPoint{Name: "senhub.icmp.rtt.min", Value: float32(res.minRTT.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "senhub.icmp.rtt.avg", Value: float32(res.avgRTT.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "senhub.icmp.rtt.max", Value: float32(res.maxRTT.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "senhub.icmp.rtt.stddev", Value: float32(res.stddevRTT.Seconds() * 1000), Timestamp: ts, Tags: baseTags},
		)
	}
	return points
}

// pingOnce is the production pingFunc: one pro-bing round per target.
func (p *ICMPCheckProbe) pingOnce(target string) pingResult {
	res := pingResult{target: target}

	pinger, err := probing.NewPinger(target)
	if err != nil {
		res.err = fmt.Errorf("resolving %s: %w", target, err)
		res.lossRatio = 1
		return res
	}
	pinger.Count = p.config.Count
	pinger.Timeout = p.config.Timeout
	pinger.Size = p.config.PacketSize
	pinger.SetPrivileged(p.config.Privileged)

	if err := pinger.Run(); err != nil {
		if !p.config.Privileged && errors.Is(err, os.ErrPermission) {
			// Datagram ICMP refused: net.ipv4.ping_group_range likely
			// excludes this process (stock Ubuntu/Debian disable it).
			// Actionable instead of a silent up=0 forever (#357).
			err = fmt.Errorf("%w (unprivileged ICMP datagram sockets are gated by net.ipv4.ping_group_range, disabled by default on Ubuntu/Debian; set privileged: true or widen the sysctl)", err)
		}
		res.err = fmt.Errorf("pinging %s: %w", target, err)
		res.lossRatio = 1
		return res
	}

	stats := pinger.Statistics()
	res.resolvedIP = stats.IPAddr.String()
	res.sent = stats.PacketsSent
	res.received = stats.PacketsRecv
	res.lossRatio = stats.PacketLoss / 100
	res.minRTT = stats.MinRtt
	res.avgRTT = stats.AvgRtt
	res.maxRTT = stats.MaxRtt
	res.stddevRTT = stats.StdDevRtt
	return res
}
