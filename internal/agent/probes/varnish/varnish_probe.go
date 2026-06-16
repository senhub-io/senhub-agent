// Package varnish implements the free-tier Varnish Cache monitoring probe.
// It runs varnishstat -j -1 (optionally with -n <instance>) and collects
// cache operations, client requests, backend connections, thread lifecycle,
// session counts, object counts, and memory allocation.
package varnish

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "varnish"

const (
	defaultVarnishstatPath = "varnishstat"
	defaultInterval        = 60 * time.Second
)

// varnishStat is one entry in the varnishstat -j output.
type varnishStat struct {
	Value float64 `json:"value"`
}

// varnishConfig holds parsed probe configuration.
type varnishConfig struct {
	VarnishstatPath string
	InstanceName    string
	Interval        time.Duration
}

// VarnishProbe collects Varnish Cache metrics via varnishstat.
type VarnishProbe struct {
	*types.BaseProbe
	cfg          varnishConfig
	moduleLogger *logger.ModuleLogger
	entitySrc    *varnishEntitySource
	unregister   func()
}

// NewVarnishProbe constructs the probe. Config errors surface here.
func NewVarnishProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.varnish")

	cfg := varnishConfig{
		VarnishstatPath: defaultVarnishstatPath,
		Interval:        defaultInterval,
	}

	if v, ok := config["varnishstat_path"].(string); ok && v != "" {
		cfg.VarnishstatPath = v
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	// Resolve the stable host id once at construction; errors are non-fatal
	// (the entity source degrades gracefully to "varnish" as a last resort).
	var hostID string
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}

	probe := &VarnishProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		entitySrc:    newVarnishEntitySource(cfg.InstanceName, hostID),
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySrc)
	return probe, nil
}

func (p *VarnishProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *VarnishProbe) ShouldStart() bool          { return true }
func (p *VarnishProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *VarnishProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("varnishstat_path", p.cfg.VarnishstatPath).
		Str("instance_name", p.cfg.InstanceName).
		Msg("starting varnish probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *VarnishProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	return nil
}

// Collect runs varnishstat and emits datapoints. If varnishstat is not found
// or returns an error, senhub.varnish.up=0 is emitted and no error is
// returned to the framework (a broken Varnish instance is a measurement, not
// a collection fault).
func (p *VarnishProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	statusTag := tags.Tag{Key: "metric_type", Value: "status"}

	stats, err := p.runVarnishstat()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("varnishstat failed; reporting varnish down")
		p.entitySrc.setReachable(false)
		points := []data_store.DataPoint{
			{Name: "senhub.varnish.up", Value: float64(0), Timestamp: now, Tags: []tags.Tag{statusTag}},
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	p.entitySrc.setReachable(true)
	points := p.buildDataPoints(stats, now)
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// runVarnishstat executes varnishstat -j -1 and parses the JSON output.
func (p *VarnishProbe) runVarnishstat() (map[string]varnishStat, error) {
	args := []string{"-j", "-1"}
	if p.cfg.InstanceName != "" {
		args = append(args, "-n", p.cfg.InstanceName)
	}

	cmd := exec.Command(p.cfg.VarnishstatPath, args...) //nolint:gosec // path is operator-configured
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running varnishstat: %w", err)
	}

	// varnishstat -j output is a JSON object where keys are either
	// metadata fields (e.g. "timestamp") or counter names.
	// We decode into a raw map and then pick the numeric entries.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing varnishstat JSON: %w", err)
	}

	stats := make(map[string]varnishStat, len(raw))
	for key, msg := range raw {
		// Skip scalar top-level fields such as "timestamp".
		if len(msg) == 0 || msg[0] != '{' {
			continue
		}
		var entry varnishStat
		if err := json.Unmarshal(msg, &entry); err != nil {
			continue
		}
		stats[key] = entry
	}
	return stats, nil
}

func get(stats map[string]varnishStat, key string) float64 {
	if s, ok := stats[key]; ok {
		return s.Value
	}
	return 0
}

// buildDataPoints converts parsed varnishstat data into DataPoints.
func (p *VarnishProbe) buildDataPoints(stats map[string]varnishStat, ts time.Time) []data_store.DataPoint {
	statusTag := tags.Tag{Key: "metric_type", Value: "status"}
	cacheTag := tags.Tag{Key: "metric_type", Value: "cache"}
	requestTag := tags.Tag{Key: "metric_type", Value: "requests"}
	connectionTag := tags.Tag{Key: "metric_type", Value: "connections"}
	threadTag := tags.Tag{Key: "metric_type", Value: "threads"}
	sessionTag := tags.Tag{Key: "metric_type", Value: "sessions"}
	objectTag := tags.Tag{Key: "metric_type", Value: "objects"}
	memoryTag := tags.Tag{Key: "metric_type", Value: "memory"}

	var points []data_store.DataPoint

	// Probe up
	points = append(points, data_store.DataPoint{
		Name:      "senhub.varnish.up",
		Value:     float64(1),
		Timestamp: ts,
		Tags:      []tags.Tag{statusTag},
	})

	// varnish.cache.operations — collapsed via result tag
	for _, entry := range []struct {
		key    string
		result string
	}{
		{"MAIN.cache_hit", "hit"},
		{"MAIN.cache_miss", "miss"},
		{"MAIN.cache_hitpass", "hitpass"},
	} {
		points = append(points, data_store.DataPoint{
			Name:      "varnish.cache.operations",
			Value:     float64(get(stats, entry.key)),
			Timestamp: ts,
			Tags:      []tags.Tag{cacheTag, {Key: "result", Value: entry.result}},
		})
	}

	// varnish.client.requests.received
	points = append(points, data_store.DataPoint{
		Name:      "varnish.client.requests.received",
		Value:     float64(get(stats, "MAIN.client_req")),
		Timestamp: ts,
		Tags:      []tags.Tag{requestTag},
	})

	// varnish.backend.connections.*
	for _, entry := range []struct {
		key    string
		metric string
	}{
		{"MAIN.backend_conn", "varnish.backend.connections.success"},
		{"MAIN.backend_fail", "varnish.backend.connections.fail"},
		{"MAIN.backend_reuse", "varnish.backend.connections.reused"},
	} {
		points = append(points, data_store.DataPoint{
			Name:      entry.metric,
			Value:     float64(get(stats, entry.key)),
			Timestamp: ts,
			Tags:      []tags.Tag{connectionTag},
		})
	}

	// varnish.thread.operations — collapsed via operation tag
	for _, entry := range []struct {
		key       string
		operation string
	}{
		{"MAIN.threads_created", "created"},
		{"MAIN.threads_destroyed", "destroyed"},
		{"MAIN.threads_failed", "failed"},
	} {
		points = append(points, data_store.DataPoint{
			Name:      "varnish.thread.operations",
			Value:     float64(get(stats, entry.key)),
			Timestamp: ts,
			Tags:      []tags.Tag{threadTag, {Key: "operation", Value: entry.operation}},
		})
	}

	// varnish.session.connections / varnish.session.dropped
	points = append(points, data_store.DataPoint{
		Name:      "varnish.session.connections",
		Value:     float64(get(stats, "MAIN.sess_conn")),
		Timestamp: ts,
		Tags:      []tags.Tag{sessionTag},
	})
	points = append(points, data_store.DataPoint{
		Name:      "varnish.session.dropped",
		Value:     float64(get(stats, "MAIN.sess_drop")),
		Timestamp: ts,
		Tags:      []tags.Tag{sessionTag},
	})

	// varnish.objects.stored (gauge)
	points = append(points, data_store.DataPoint{
		Name:      "varnish.objects.stored",
		Value:     float64(get(stats, "MAIN.n_object")),
		Timestamp: ts,
		Tags:      []tags.Tag{objectTag},
	})

	// varnish.memory.allocated — sum all SMA.*.g_bytes and SMF.*.g_bytes entries
	var totalMemory float64
	for key, stat := range stats {
		if (strings.HasPrefix(key, "SMA.") || strings.HasPrefix(key, "SMF.")) && strings.HasSuffix(key, ".g_bytes") {
			totalMemory += stat.Value
		}
	}
	points = append(points, data_store.DataPoint{
		Name:      "varnish.memory.allocated",
		Value:     float64(totalMemory),
		Timestamp: ts,
		Tags:      []tags.Tag{memoryTag},
	})

	return points
}
