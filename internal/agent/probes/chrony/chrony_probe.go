// Package chrony implements the free chrony probe: NTP synchronisation
// health via chronyc tracking. Monitors stratum, time offset, frequency
// offset, skew, root delay and root dispersion — the core NTP quality
// indicators that signal drift, mis-configured time sources, or a host
// that has fallen out of sync.
//
// The probe shells out to `chronyc -c tracking` (machine-readable CSV)
// once per interval and parses the 13 comma-separated fields. If
// chronyc is not found or returns a non-zero exit, senhub.chrony.up=0
// is emitted and all other metrics are suppressed for that cycle.
//
// NTP state changes slowly; the default interval is 30 s.
package chrony

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "chrony"

const (
	defaultInterval    = 30 * time.Second
	defaultChronyc     = "chronyc"
	maxOutputBytes     = 4 * 1024
)

// leapStatus values returned by chronyc -c tracking (field 12).
const (
	leapNormal    = "Normal"
	leapInsert    = "Insert second"
	leapDelete    = "Delete second"
	leapNotSynced = "Not synchronised"
)

type chronyConfig struct {
	ChronyPath string
	Interval   time.Duration
}

// trackingResult holds one parsed chronyc tracking output.
type trackingResult struct {
	// raw CSV line — used in tests.
	raw []string

	stratum         float32
	systemTimeS     float64 // seconds (converted to ms for the metric)
	freqPPM         float64
	skewPPM         float64
	rootDelayS      float64 // seconds (converted to ms for the metric)
	rootDispersionS float64 // seconds (converted to ms for the metric)
	leapStatus      string

	err error
}

type runFunc func() trackingResult

// ChronyProbe monitors NTP synchronisation via chronyc.
type ChronyProbe struct {
	*types.BaseProbe
	cfg          chronyConfig
	moduleLogger *logger.ModuleLogger
	run          runFunc
}

// NewChronyProbe constructs the probe. All config defaults are applied
// here so that a zero-config block (`params: {}`) gives sensible values.
func NewChronyProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.chrony")

	cfg := chronyConfig{
		ChronyPath: defaultChronyc,
		Interval:   defaultInterval,
	}
	if v, ok := config["chronyc_path"].(string); ok && v != "" {
		cfg.ChronyPath = v
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	p := &ChronyProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
	}
	p.SetProbeType(ProbeType)
	p.run = p.runOnce
	return p, nil
}

func (p *ChronyProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *ChronyProbe) ShouldStart() bool          { return true }
func (p *ChronyProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *ChronyProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("chronyc_path", p.cfg.ChronyPath).
		Msg("Starting chrony probe")
	return nil
}

func (p *ChronyProbe) OnShutdown(_ context.Context) error { return nil }

// Collect runs chronyc tracking once and emits the NTP metrics.
// On subprocess failure senhub.chrony.up=0 is the only point emitted.
func (p *ChronyProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{{Key: "metric_type", Value: "time_sync"}}

	res := p.run()

	upValue := float32(1)
	if res.err != nil {
		upValue = 0
		p.moduleLogger.Warn().Err(res.err).Msg("chronyc tracking failed")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.chrony.up", Value: upValue, Timestamp: now, Tags: baseTags},
	}

	if res.err != nil {
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	points = append(points,
		data_store.DataPoint{
			Name:      "ntp.time.offset",
			Value:     float32(res.systemTimeS * 1000),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.frequency.offset",
			Value:     float32(res.freqPPM),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.skew",
			Value:     float32(res.skewPPM),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.root.delay",
			Value:     float32(res.rootDelayS * 1000),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.root.dispersion",
			Value:     float32(res.rootDispersionS * 1000),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.stratum",
			Value:     res.stratum,
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.leap_status",
			Value:     leapToFloat(res.leapStatus),
			Timestamp: now,
			Tags:      baseTags,
		},
	)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// leapToFloat converts the chronyc leap-status string to the numeric
// value used in the ntp.leap_status metric.
//   - Normal        → 0
//   - Insert second → 1
//   - Delete second → 2
//   - Not synchronised → 3
func leapToFloat(status string) float32 {
	switch status {
	case leapNormal:
		return 0
	case leapInsert:
		return 1
	case leapDelete:
		return 2
	default:
		// leapNotSynced or anything unexpected
		return 3
	}
}

// runOnce is the production runFunc: spawn chronyc, parse output.
func (p *ChronyProbe) runOnce() trackingResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.cfg.ChronyPath, "-c", "tracking")
	var out bytes.Buffer
	cmd.Stdout = &cappedWriter{buf: &out, max: maxOutputBytes}

	if err := cmd.Run(); err != nil {
		return trackingResult{err: fmt.Errorf("chronyc: %w", err)}
	}

	line := strings.TrimRight(out.String(), "\r\n")
	return parseTracking(line)
}

// parseTracking converts one chronyc -c tracking CSV line into a
// trackingResult. Field order per chrony documentation:
//
//	0  reference_id
//	1  stratum
//	2  ref_time
//	3  system_time     (seconds, + = fast, - = slow)
//	4  last_offset
//	5  rms_offset
//	6  freq_ppm
//	7  residual_freq
//	8  skew
//	9  root_delay      (seconds)
//	10 root_dispersion (seconds)
//	11 update_interval
//	12 leap_status
func parseTracking(line string) trackingResult {
	fields := strings.Split(line, ",")
	if len(fields) < 13 {
		return trackingResult{
			err: fmt.Errorf("chronyc tracking: expected 13 fields, got %d (line: %q)", len(fields), line),
		}
	}

	stratum, err := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
	if err != nil {
		return trackingResult{err: fmt.Errorf("chronyc: parsing stratum: %w", err)}
	}
	systemTime, err := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64)
	if err != nil {
		return trackingResult{err: fmt.Errorf("chronyc: parsing system_time: %w", err)}
	}
	freqPPM, err := strconv.ParseFloat(strings.TrimSpace(fields[6]), 64)
	if err != nil {
		return trackingResult{err: fmt.Errorf("chronyc: parsing freq_ppm: %w", err)}
	}
	skew, err := strconv.ParseFloat(strings.TrimSpace(fields[8]), 64)
	if err != nil {
		return trackingResult{err: fmt.Errorf("chronyc: parsing skew: %w", err)}
	}
	rootDelay, err := strconv.ParseFloat(strings.TrimSpace(fields[9]), 64)
	if err != nil {
		return trackingResult{err: fmt.Errorf("chronyc: parsing root_delay: %w", err)}
	}
	rootDisp, err := strconv.ParseFloat(strings.TrimSpace(fields[10]), 64)
	if err != nil {
		return trackingResult{err: fmt.Errorf("chronyc: parsing root_dispersion: %w", err)}
	}

	return trackingResult{
		raw:             fields,
		stratum:         float32(stratum),
		systemTimeS:     systemTime,
		freqPPM:         freqPPM,
		skewPPM:         skew,
		rootDelayS:      rootDelay,
		rootDispersionS: rootDisp,
		leapStatus:      strings.TrimSpace(fields[12]),
	}
}

// cappedWriter limits the bytes captured from chronyc stdout.
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
