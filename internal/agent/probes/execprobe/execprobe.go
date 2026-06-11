// Package execprobe implements the free exec probe: run an
// operator-supplied program on interval and turn its output into
// metrics — the custom-sensor long tail every PRTG estate ends in
// (#305). Two output contracts are parsed: the Nagios plugin
// convention (exit code + perfdata) so existing check_* plugins work
// unchanged, and a JSON contract for new scripts.
//
// Security posture: the program runs as the agent user (root on
// Linux, see #223). To keep "configure a probe" from silently meaning
// "run anything as root", the probe refuses relative command paths
// and, on Unix, world-writable executables. No shell is involved —
// the command and its arguments are passed verbatim to the OS; shell
// pipelines belong in a script file the operator owns.
//
// Runaway protection: a hard timeout kills the whole process group
// (Setpgid on Unix, mirroring the linux_logs subprocess handling) and
// a concurrent-run guard skips the cycle if the previous run is still
// going instead of piling up processes.
package execprobe

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "exec"

const (
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second
	// maxOutputBytes caps captured stdout/stderr; a chatty script must
	// not exhaust the agent's memory.
	maxOutputBytes = 1 << 20
)

// execResult is the per-run outcome the datapoint builder consumes.
type execResult struct {
	exitCode int // Nagios semantics: 0 ok, 1 warning, 2 critical, 3+ unknown
	stdout   []byte
	duration time.Duration
	timedOut bool
	err      error // spawn-level failure (not a non-zero exit)
}

type runFunc func() execResult

type ExecProbe struct {
	*types.BaseProbe
	config       execConfig
	moduleLogger *logger.ModuleLogger
	run          runFunc
	running      sync.Mutex // concurrent-run guard; TryLock per cycle
}

type execConfig struct {
	Command  string
	Args     []string
	Format   string // "nagios" or "json"
	Interval time.Duration
	Timeout  time.Duration
	Env      map[string]string
	WorkDir  string
}

// NewExecProbe constructs the probe. Config errors surface here, and
// the security checks on the command path are part of construction:
// a probe that would run an unsafe target must never start.
func NewExecProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.exec")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}
	if err := checkExecutable(cfg.Command); err != nil {
		return nil, err
	}

	probe := &ExecProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
	}
	probe.SetProbeType(ProbeType)
	probe.run = probe.runOnce
	return probe, nil
}

func parseConfig(config map[string]interface{}) (execConfig, error) {
	cfg := execConfig{
		Format:   "nagios",
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
	}

	cmd, ok := config["command"].(string)
	if !ok || cmd == "" {
		return cfg, fmt.Errorf("exec requires a command (absolute path to the program)")
	}
	if !filepath.IsAbs(cmd) {
		return cfg, fmt.Errorf("exec command must be an absolute path (got %q); PATH lookup is not allowed", cmd)
	}
	cfg.Command = cmd

	switch raw := config["args"].(type) {
	case nil:
	case []interface{}:
		for _, item := range raw {
			s, ok := item.(string)
			if !ok {
				return cfg, fmt.Errorf("exec args must be strings (got %T)", item)
			}
			cfg.Args = append(cfg.Args, s)
		}
	case []string:
		cfg.Args = raw
	default:
		return cfg, fmt.Errorf("exec args must be a list (got %T)", raw)
	}

	if v, ok := config["format"].(string); ok && v != "" {
		if v != "nagios" && v != "json" {
			return cfg, fmt.Errorf("exec format must be \"nagios\" or \"json\" (got %q)", v)
		}
		cfg.Format = v
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["workdir"].(string); ok {
		cfg.WorkDir = v
	}
	if raw, ok := config["env"].(map[string]interface{}); ok {
		cfg.Env = make(map[string]string, len(raw))
		for k, val := range raw {
			s, ok := val.(string)
			if !ok {
				return cfg, fmt.Errorf("exec env values must be strings (got %T for %s)", val, k)
			}
			cfg.Env[k] = s
		}
	}
	return cfg, nil
}

func (p *ExecProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *ExecProbe) ShouldStart() bool          { return true }
func (p *ExecProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *ExecProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Str("command", p.config.Command).
		Str("format", p.config.Format).
		Msg("Starting exec probe")
	return nil
}

func (p *ExecProbe) OnShutdown(ctx context.Context) error { return nil }

// Collect runs the configured program once. A failing or non-zero
// check is a measurement (status reflects it), never a collection
// error. If the previous run is still going, the cycle is skipped and
// reported, not stacked.
func (p *ExecProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{{Key: "metric_type", Value: "exec"}}

	if !p.running.TryLock() {
		p.moduleLogger.Warn().
			Str("command", p.config.Command).
			Msg("previous exec run still in progress; skipping this cycle")
		return p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{
			{Name: "senhub.exec.skipped", Value: 1, Timestamp: now, Tags: baseTags},
		}, p.GetName()), nil
	}
	defer p.running.Unlock()

	res := p.run()

	points := []data_store.DataPoint{
		{Name: "senhub.exec.duration", Value: float32(res.duration.Seconds() * 1000), Timestamp: now, Tags: baseTags},
	}

	if res.err != nil {
		// Spawn-level failure (missing file, permission): status unknown.
		p.moduleLogger.Warn().Err(res.err).Str("command", p.config.Command).Msg("exec run failed")
		points = append(points,
			data_store.DataPoint{Name: "senhub.exec.status", Value: 3, Timestamp: now, Tags: baseTags},
		)
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	timedOut := float32(0)
	if res.timedOut {
		timedOut = 1
	}
	points = append(points,
		data_store.DataPoint{Name: "senhub.exec.timeout", Value: timedOut, Timestamp: now, Tags: baseTags},
	)

	switch p.config.Format {
	case "json":
		status, metrics, parseErr := parseJSONOutput(res.stdout, res.exitCode)
		if parseErr != nil {
			p.moduleLogger.Warn().Err(parseErr).Str("command", p.config.Command).Msg("exec JSON output unparseable")
			status = 3
		}
		points = append(points,
			data_store.DataPoint{Name: "senhub.exec.status", Value: float32(status), Timestamp: now, Tags: baseTags},
		)
		points = append(points, metricsToDatapoints(metrics, now)...)
	default: // nagios
		status := nagiosStatus(res.exitCode, res.timedOut)
		points = append(points,
			data_store.DataPoint{Name: "senhub.exec.status", Value: float32(status), Timestamp: now, Tags: baseTags},
		)
		perfdata := parseNagiosPerfdata(res.stdout)
		points = append(points, metricsToDatapoints(perfdata, now)...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// nagiosStatus maps an exit code to the plugin convention; anything
// outside 0..2 (including a timeout kill) is unknown.
func nagiosStatus(exitCode int, timedOut bool) int {
	if timedOut {
		return 3
	}
	if exitCode >= 0 && exitCode <= 2 {
		return exitCode
	}
	return 3
}

// metricsToDatapoints converts parsed check metrics into typed
// pass-through datapoints (the otel_type tag carries counter/gauge
// semantics to the mapper; names cannot be pre-enumerated in a
// transformer YAML, same mechanism as prometheus_scrape).
func metricsToDatapoints(metrics []checkMetric, ts time.Time) []data_store.DataPoint {
	points := make([]data_store.DataPoint, 0, len(metrics))
	for _, m := range metrics {
		t := []tags.Tag{
			{Key: "metric_type", Value: "exec"},
			{Key: "otel_type", Value: m.otelType},
		}
		for k, v := range m.tags {
			if v != "" {
				t = append(t, tags.Tag{Key: k, Value: v})
			}
		}
		points = append(points, data_store.DataPoint{
			Name:      m.name,
			Value:     float32(m.value),
			Timestamp: ts,
			Tags:      t,
		})
	}
	return points
}

// runOnce is the production runFunc: one process, hard deadline.
func (p *ExecProbe) runOnce() execResult {
	res := execResult{}

	ctx, cancel := context.WithTimeout(context.Background(), p.config.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.config.Command, p.config.Args...)
	configureSysProcAttr(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	if p.config.WorkDir != "" {
		cmd.Dir = p.config.WorkDir
	}
	if len(p.config.Env) > 0 {
		cmd.Env = buildEnv(p.config.Env)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &cappedWriter{buf: &stdout, max: maxOutputBytes}
	cmd.Stderr = &cappedWriter{buf: &stderr, max: maxOutputBytes}

	start := time.Now()
	err := cmd.Run()
	res.duration = time.Since(start)
	res.stdout = stdout.Bytes()
	res.timedOut = ctx.Err() == context.DeadlineExceeded

	switch e := err.(type) {
	case nil:
		res.exitCode = 0
	case *exec.ExitError:
		res.exitCode = e.ExitCode()
	default:
		res.err = fmt.Errorf("running %s: %w", p.config.Command, err)
	}
	if res.timedOut {
		p.moduleLogger.Warn().
			Str("command", p.config.Command).
			Dur("timeout", p.config.Timeout).
			Msg("exec run killed on timeout")
	}
	if res.exitCode != 0 && stderr.Len() > 0 {
		p.moduleLogger.Debug().
			Str("command", p.config.Command).
			Str("stderr", stderr.String()).
			Msg("exec stderr")
	}
	return res
}

// cappedWriter keeps at most max bytes and silently discards the rest.
type cappedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if remaining := w.max - w.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			w.buf.Write(p[:remaining])
		} else {
			w.buf.Write(p)
		}
	}
	return len(p), nil
}
