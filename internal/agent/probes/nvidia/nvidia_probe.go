// Package nvidia implements the free nvidia probe: NVIDIA GPU monitoring via
// nvidia-smi. Works on Linux and Windows wherever the NVIDIA driver ships the
// nvidia-smi utility. When nvidia-smi is absent or fails, the probe emits
// senhub.nvidia.up=0 and returns nil — silent degradation on hosts without a
// GPU keeps the probe safe to enable broadly.
//
// GPUs are host hardware: they are facets of the host entity, not distinct
// service instances. Metrics carry host.id (via common.GetHostTags) and join
// the host entity emitted by the foundation detector — same doctrine as
// cpu/memory/logicaldisk.
package nvidia

import (
	"bytes"
	"context"
	"encoding/csv"
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

// ProbeType is the stable technical identifier (license claims, transformer
// file name, registry key). Never rename without a migration plan.
const ProbeType = "nvidia"

const (
	defaultInterval    = 30 * time.Second
	defaultNvidiaSmiPath = "nvidia-smi"
	smiTimeout         = 15 * time.Second
)

// nvidiaGPU holds parsed fields from one nvidia-smi CSV row.
type nvidiaGPU struct {
	index               string
	name                string
	uuid                string
	utilizationGPU      float64 // percent 0-100
	utilizationMemory   float64 // percent 0-100
	memoryUsedMiB       float64 // MiB
	memoryTotalMiB      float64 // MiB
	temperatureGPU      float64 // Celsius
	powerDraw           float64 // W
	powerLimit          float64 // W
	fanSpeed            float64 // percent 0-100
	utilizationEncoder  float64 // percent 0-100
	utilizationDecoder  float64 // percent 0-100
}

// runSmiFunc is the injectable command runner (production = runSmi, tests
// substitute their own).
type runSmiFunc func(path string) ([]byte, error)

// NvidiaProbe collects GPU metrics from nvidia-smi.
type NvidiaProbe struct {
	*types.BaseProbe
	config       probeConfig
	moduleLogger *logger.ModuleLogger
	runSmi       runSmiFunc
}

type probeConfig struct {
	NvidiaSmiPath string
	Interval      time.Duration
	GPUs          []string // empty = all
}

// NewNvidiaProbe builds an nvidia probe from its raw params block.
func NewNvidiaProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg := parseConfig(rawConfig)
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.nvidia")

	probe := &NvidiaProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		runSmi:       defaultRunSmi,
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func parseConfig(raw map[string]interface{}) probeConfig {
	cfg := probeConfig{
		NvidiaSmiPath: defaultNvidiaSmiPath,
		Interval:      defaultInterval,
	}
	if v, ok := raw["nvidia_smi_path"].(string); ok && v != "" {
		cfg.NvidiaSmiPath = v
	}
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	switch v := raw["gpus"].(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				cfg.GPUs = append(cfg.GPUs, s)
			}
		}
	case []string:
		cfg.GPUs = v
	}
	return cfg
}

func (p *NvidiaProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *NvidiaProbe) ShouldStart() bool          { return true }
func (p *NvidiaProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *NvidiaProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("nvidia_smi_path", p.config.NvidiaSmiPath).
		Msg("Starting nvidia probe")
	return nil
}

func (p *NvidiaProbe) OnShutdown(_ context.Context) error {
	return nil
}

// Collect runs nvidia-smi once and turns each GPU row into datapoints. When
// nvidia-smi is absent or fails the probe emits senhub.nvidia.up=0 only and
// returns nil — not an error — so the agent does not mark the probe unhealthy
// on a GPU-less host.
func (p *NvidiaProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	hostTags, err := common.GetHostTags()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("failed to get host tags")
		hostTags = nil
	}

	output, smiErr := p.runSmi(p.config.NvidiaSmiPath)
	if smiErr != nil {
		p.moduleLogger.Warn().Err(smiErr).Str("path", p.config.NvidiaSmiPath).
			Msg("nvidia-smi failed; emitting up=0")
		upTags := append(hostTags, tags.Tag{Key: "metric_type", Value: "availability"})
		point := data_store.DataPoint{Name: "senhub.nvidia.up", Value: 0, Timestamp: now, Tags: upTags}
		return p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{point}, p.GetName()), nil
	}

	gpus, parseErr := parseSmiOutput(output)
	if parseErr != nil {
		p.moduleLogger.Warn().Err(parseErr).Msg("nvidia-smi output parse failed; emitting up=0")
		upTags := append(hostTags, tags.Tag{Key: "metric_type", Value: "availability"})
		point := data_store.DataPoint{Name: "senhub.nvidia.up", Value: 0, Timestamp: now, Tags: upTags}
		return p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{point}, p.GetName()), nil
	}

	gpus = filterGPUs(gpus, p.config.GPUs)

	var points []data_store.DataPoint
	for _, gpu := range gpus {
		points = append(points, buildDatapoints(gpu, hostTags, now)...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// buildDatapoints converts one parsed GPU row into the full metric set.
// hostTags carries host.id and related resource attributes so the metrics
// join the host entity in Toise (same convention as cpu/memory/logicaldisk).
func buildDatapoints(gpu nvidiaGPU, hostTags []tags.Tag, ts time.Time) []data_store.DataPoint {
	base := make([]tags.Tag, 0, len(hostTags)+4)
	base = append(base, hostTags...)
	base = append(base,
		tags.Tag{Key: "gpu.index", Value: gpu.index},
		tags.Tag{Key: "gpu.name", Value: gpu.name},
		tags.Tag{Key: "gpu.uuid", Value: gpu.uuid},
		tags.Tag{Key: "metric_type", Value: "gpu"},
	)

	tag := func(extra ...tags.Tag) []tags.Tag {
		out := make([]tags.Tag, len(base)+len(extra))
		copy(out, base)
		copy(out[len(base):], extra)
		return out
	}

	pts := []data_store.DataPoint{
		{Name: "senhub.nvidia.up", Value: 1, Timestamp: ts, Tags: tag()},
		{Name: "gpu.utilization", Value: float32(gpu.utilizationGPU / 100), Timestamp: ts, Tags: tag()},
		{Name: "gpu.memory.used", Value: float32(gpu.memoryUsedMiB * 1024 * 1024), Timestamp: ts, Tags: tag()},
		{Name: "gpu.memory.total", Value: float32(gpu.memoryTotalMiB * 1024 * 1024), Timestamp: ts, Tags: tag()},
		{Name: "gpu.memory.utilization", Value: float32(gpu.utilizationMemory / 100), Timestamp: ts, Tags: tag()},
		{Name: "gpu.temperature", Value: float32(gpu.temperatureGPU), Timestamp: ts, Tags: tag()},
		{Name: "gpu.encoder.utilization", Value: float32(gpu.utilizationEncoder / 100), Timestamp: ts, Tags: tag()},
		{Name: "gpu.decoder.utilization", Value: float32(gpu.utilizationDecoder / 100), Timestamp: ts, Tags: tag()},
		{Name: "gpu.fan.speed", Value: float32(gpu.fanSpeed / 100), Timestamp: ts, Tags: tag()},
	}
	// Power metrics are optional: nvidia-smi reports "N/A" for cards that
	// don't expose the power management interface (some laptop GPUs).
	if gpu.powerDraw >= 0 {
		pts = append(pts, data_store.DataPoint{Name: "gpu.power.usage", Value: float32(gpu.powerDraw), Timestamp: ts, Tags: tag()})
	}
	if gpu.powerLimit >= 0 {
		pts = append(pts, data_store.DataPoint{Name: "gpu.power.limit", Value: float32(gpu.powerLimit), Timestamp: ts, Tags: tag()})
	}
	return pts
}

// filterGPUs returns only the GPUs whose index appears in the allow-list.
// An empty list means all GPUs pass.
func filterGPUs(gpus []nvidiaGPU, allowed []string) []nvidiaGPU {
	if len(allowed) == 0 {
		return gpus
	}
	set := make(map[string]bool, len(allowed))
	for _, idx := range allowed {
		set[idx] = true
	}
	out := gpus[:0]
	for _, g := range gpus {
		if set[g.index] {
			out = append(out, g)
		}
	}
	return out
}

// parseSmiOutput parses the CSV output of the nvidia-smi query command.
//
// Column order matches the --query-gpu argument in defaultRunSmi:
//
//	index, name, uuid,
//	utilization.gpu, utilization.memory,
//	memory.used, memory.total,
//	temperature.gpu,
//	power.draw, power.limit,
//	fan.speed,
//	encoder.stats.sessionCount (unused, present for format stability),
//	utilization.encoder, utilization.decoder
func parseSmiOutput(output []byte) ([]nvidiaGPU, error) {
	r := csv.NewReader(bytes.NewReader(output))
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing nvidia-smi CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}

	const minCols = 14
	gpus := make([]nvidiaGPU, 0, len(records))
	for i, rec := range records {
		if len(rec) < minCols {
			return nil, fmt.Errorf("row %d: expected %d columns, got %d", i, minCols, len(rec))
		}

		gpu := nvidiaGPU{
			index: strings.TrimSpace(rec[0]),
			name:  strings.TrimSpace(rec[1]),
			uuid:  strings.TrimSpace(rec[2]),
			// power fields default to -1 (sentinel: N/A from driver)
			powerDraw:  -1,
			powerLimit: -1,
		}

		gpu.utilizationGPU = parseFloat(rec[3])
		gpu.utilizationMemory = parseFloat(rec[4])
		gpu.memoryUsedMiB = parseFloat(rec[5])
		gpu.memoryTotalMiB = parseFloat(rec[6])
		gpu.temperatureGPU = parseFloat(rec[7])

		if pd := parseFloatNA(rec[8]); pd >= 0 {
			gpu.powerDraw = pd
		}
		if pl := parseFloatNA(rec[9]); pl >= 0 {
			gpu.powerLimit = pl
		}

		gpu.fanSpeed = parseFloatNA2(rec[10])
		// rec[11] = encoder.stats.sessionCount — not used as a metric
		gpu.utilizationEncoder = parseFloat(rec[12])
		gpu.utilizationDecoder = parseFloat(rec[13])

		gpus = append(gpus, gpu)
	}
	return gpus, nil
}

// parseFloat parses a trimmed numeric string from nvidia-smi. Returns 0 on any
// parse error (nvidia-smi guarantees numeric output for these fields).
func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseFloatNA parses a field that may be "[N/A]" when the GPU does not
// support it (e.g. power management on some laptop GPUs). Returns -1 when the
// value is absent or unparseable.
func parseFloatNA(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "N/A") || strings.Contains(s, "[N/A]") {
		return -1
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return -1
	}
	return v
}

// parseFloatNA2 is like parseFloatNA but returns 0 instead of -1 on absence
// (fan speed: a missing value is treated as 0 rather than "not applicable").
func parseFloatNA2(s string) float64 {
	v := parseFloatNA(s)
	if v < 0 {
		return 0
	}
	return v
}

// defaultRunSmi runs nvidia-smi with the standard query flags.
// The query column order MUST match parseSmiOutput.
func defaultRunSmi(path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), smiTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path,
		"--query-gpu=index,name,uuid,utilization.gpu,utilization.memory,"+
			"memory.used,memory.total,temperature.gpu,power.draw,power.limit,"+
			"fan.speed,encoder.stats.sessionCount,utilization.encoder,utilization.decoder",
		"--format=csv,noheader,nounits",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running %s: %w", path, err)
	}
	return out.Bytes(), nil
}
