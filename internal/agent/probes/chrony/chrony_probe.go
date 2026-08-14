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
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "chrony"

const (
	defaultInterval = 30 * time.Second
	defaultChronyc  = "chronyc"
	maxOutputBytes  = 4 * 1024
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

	stratum         float64
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
	entitySrc    *chronyEntitySource
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
	p.entitySrc = newChronyEntitySource()
	p.SetEntitySource(p.entitySrc)
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

func (p *ChronyProbe) OnShutdown(_ context.Context) error {
	return nil
}

// Collect runs chronyc tracking once and emits the NTP metrics.
// On subprocess failure senhub.chrony.up=0 is the only point emitted.
func (p *ChronyProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{{Key: "metric_type", Value: "time_sync"}}

	hostID := ""
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}

	res := p.run()

	upValue := float64(1)
	if res.err != nil {
		upValue = 0
		p.moduleLogger.Warn().Err(res.err).Msg("chronyc tracking failed")
		p.entitySrc.setReachable(false, "", hostID)
	}

	points := []data_store.DataPoint{
		{Name: "senhub.chrony.up", Value: upValue, Timestamp: now, Tags: baseTags},
	}

	// Why up is 0, as its own series.
	//
	// Reported by an operator: chronyc absent and chronyc unreadable produced
	// the identical observable — a probe listed as active emitting only its own
	// up=0 — so "there is no chrony here" and "the probe cannot read chrony"
	// were indistinguishable without opening the agent log. One of those needs
	// action and the other does not.
	//
	// One-hot rather than an enum value: a reason is a string, a string cannot
	// be a metric value, and a dashboard should be able to count hosts in a
	// state without decoding a number.
	for _, r := range []string{reasonOK, reasonNotInstalled, reasonExecFailed, reasonParseFailed} {
		v := float64(0)
		if r == reasonOf(res.err) {
			v = 1
		}
		t := append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "reason", Value: r})
		points = append(points, data_store.DataPoint{
			Name: "senhub.chrony.state", Value: v, Timestamp: now, Tags: t,
		})
	}

	if res.err != nil {
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}
	p.entitySrc.setReachable(true, "", hostID)

	points = append(points,
		data_store.DataPoint{
			Name:      "ntp.time.offset",
			Value:     float64(res.systemTimeS * 1000),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.frequency.offset",
			Value:     float64(res.freqPPM),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.skew",
			Value:     float64(res.skewPPM),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.root.delay",
			Value:     float64(res.rootDelayS * 1000),
			Timestamp: now,
			Tags:      baseTags,
		},
		data_store.DataPoint{
			Name:      "ntp.root.dispersion",
			Value:     float64(res.rootDispersionS * 1000),
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
func leapToFloat(status string) float64 {
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
//
// Field positions in `chronyc -c tracking`, which emits FOURTEEN
// comma-separated values:
//
//	65D8456C,2620:2d:4000:1::3123,3,1786708478.06,0.000106,-0.000040,…,Normal
//	   0              1           2       3           4        5
//	 refid        address     stratum  ref time  system time  last offset
//
// Every index here used to be one lower, and the length check demanded 13
// instead of 14 — so the parser read the reference ADDRESS as the stratum and
// every subsequent value off by one. The probe therefore never worked against
// real chronyc output: it failed with "parsing stratum: invalid syntax" naming
// an IP address, and only on a host whose clock was actually synchronised,
// because an unsynchronised chrony leaves the address empty.
//
// The test that should have caught it invented a 13-field line with no address
// column at all, so it proved the parser matched the invention rather than the
// tool. The fixture is now a verbatim capture from chrony 4.5 (#chrony-parse).
const (
	fieldStratum        = 2
	fieldSystemTime     = 4
	fieldFreqPPM        = 7
	fieldSkew           = 9
	fieldRootDelay      = 10
	fieldRootDispersion = 11
	fieldLeapStatus     = 13
	trackingFieldCount  = 14
)

func parseTracking(line string) trackingResult {
	fields := strings.Split(line, ",")
	if len(fields) < trackingFieldCount {
		return trackingResult{
			err: fmt.Errorf("chronyc tracking: expected %d fields, got %d (line: %q)", trackingFieldCount, len(fields), line),
		}
	}

	num := func(idx int, name string) (float64, error) {
		v, err := strconv.ParseFloat(strings.TrimSpace(fields[idx]), 64)
		if err != nil {
			// The offending value rides in the message. That is what made this
			// defect diagnosable from a single log line by the operator who
			// reported it: "parsing stratum" naming an IP address says
			// immediately that the column is wrong, not the data.
			return 0, fmt.Errorf("chronyc: parsing %s from field %d (%q): %w", name, idx, strings.TrimSpace(fields[idx]), err)
		}
		return v, nil
	}

	stratum, err := num(fieldStratum, "stratum")
	if err != nil {
		return trackingResult{err: err}
	}
	systemTime, err := num(fieldSystemTime, "system_time")
	if err != nil {
		return trackingResult{err: err}
	}
	freqPPM, err := num(fieldFreqPPM, "freq_ppm")
	if err != nil {
		return trackingResult{err: err}
	}
	skew, err := num(fieldSkew, "skew")
	if err != nil {
		return trackingResult{err: err}
	}
	rootDelay, err := num(fieldRootDelay, "root_delay")
	if err != nil {
		return trackingResult{err: err}
	}
	rootDisp, err := num(fieldRootDispersion, "root_dispersion")
	if err != nil {
		return trackingResult{err: err}
	}

	return trackingResult{
		raw:             fields,
		stratum:         stratum,
		systemTimeS:     systemTime,
		freqPPM:         freqPPM,
		skewPPM:         skew,
		rootDelayS:      rootDelay,
		rootDispersionS: rootDisp,
		leapStatus:      strings.TrimSpace(fields[fieldLeapStatus]),
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

// Reasons a chrony reading failed, as one-hot state series.
const (
	reasonOK           = "ok"
	reasonNotInstalled = "not_installed"
	reasonExecFailed   = "exec_failed"
	reasonParseFailed  = "parse_failed"
)

// reasonOf classifies a collection failure so an operator can tell a host
// without chrony from a host whose chrony cannot be read.
//
// The distinction matters because only one of them is a defect: a machine with
// no NTP daemon is a deployment choice, while a parse failure means the agent
// is looking at output it does not understand — which is how the field-offset
// bug stayed invisible, since both looked like "up=0" from outside.
func reasonOf(err error) string {
	if err == nil {
		return reasonOK
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "executable file not found"), strings.Contains(msg, "no such file"):
		return reasonNotInstalled
	case strings.Contains(msg, "chronyc tracking:"), strings.Contains(msg, "chronyc: parsing"):
		return reasonParseFailed
	default:
		return reasonExecFailed
	}
}
