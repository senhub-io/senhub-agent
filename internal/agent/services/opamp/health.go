package opamp

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
)

type probeInput struct {
	Name    string
	Enabled bool
	State   agentstate.ProbeRunState
}

type outputInput struct {
	Name      string
	Failure   *agentstate.StrategyFailure
	Failing   bool
	LastError string
}

type healthInput struct {
	Probes    []probeInput
	Outputs   []outputInput
	StartTime time.Time
	Now       time.Time
}

func buildHealth(in healthInput, redact func(string) string) *protobufs.ComponentHealth {
	if redact == nil {
		redact = func(s string) string { return s }
	}

	root := &protobufs.ComponentHealth{
		Healthy:            true,
		StartTimeUnixNano:  uint64(in.StartTime.UnixNano()),
		StatusTimeUnixNano: uint64(in.Now.UnixNano()),
	}

	components := make(map[string]*protobufs.ComponentHealth)
	hasStarting := false

	seenProbes := make(map[string]bool)
	for _, p := range in.Probes {
		key := "probe:" + p.Name
		if seenProbes[key] {
			continue
		}
		seenProbes[key] = true

		c := buildProbeComponent(p, in.Now, redact)
		components[key] = c
		if c.GetStatus() == "starting" {
			hasStarting = true
		}
		if !c.GetHealthy() {
			root.Healthy = false
		}
	}

	seenOutputs := make(map[string]bool)
	for _, o := range in.Outputs {
		key := "output:" + o.Name
		if seenOutputs[key] {
			continue
		}
		seenOutputs[key] = true

		c := buildOutputComponent(o, in.Now, redact)
		components[key] = c
		if !c.GetHealthy() {
			root.Healthy = false
		}
	}

	root.ComponentHealthMap = components

	if !root.Healthy {
		root.Status = "degraded"
	} else if hasStarting {
		root.Status = "starting"
	} else {
		root.Status = "running"
	}
	root.LastError = ""

	return root
}

func buildProbeComponent(in probeInput, now time.Time, redact func(string) string) *protobufs.ComponentHealth {
	c := &protobufs.ComponentHealth{
		StatusTimeUnixNano: uint64(now.UnixNano()),
	}
	if !in.State.StartedAt.IsZero() {
		c.StartTimeUnixNano = uint64(in.State.StartedAt.UnixNano())
	}

	switch {
	case !in.Enabled:
		c.Healthy = true
		c.Status = "disabled"
	case in.State.Running && in.State.Health == "failed":
		c.Healthy = false
		c.Status = "failed"
		c.LastError = redact(in.State.LastError)
	case in.State.Running:
		c.Healthy = true
		c.Status = "running"
	case !in.State.Running && in.State.Health == "failed":
		c.Healthy = false
		c.Status = "retrying"
		c.LastError = redact(in.State.LastError)
	default:
		c.Healthy = true
		c.Status = "starting"
	}

	return c
}

func buildOutputComponent(in outputInput, now time.Time, redact func(string) string) *protobufs.ComponentHealth {
	c := &protobufs.ComponentHealth{
		StatusTimeUnixNano: uint64(now.UnixNano()),
	}

	if in.Failure != nil {
		reason := in.Failure.Reason
		if in.Failure.Detail != "" {
			reason += ": " + in.Failure.Detail
		}
		c.Healthy = false
		c.Status = "failed"
		c.LastError = redact(reason)
		return c
	}

	if in.Failing {
		c.Healthy = false
		c.Status = "degraded"
		c.LastError = redact(in.LastError)
		return c
	}

	c.Healthy = true
	c.Status = "running"
	return c
}

func healthFingerprint(h *protobufs.ComponentHealth) string {
	if h == nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "root healthy=%t status=%q\n", h.GetHealthy(), h.GetStatus())

	m := h.GetComponentHealthMap()
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		c := m[key]
		if c == nil {
			continue
		}
		fmt.Fprintf(&b, "component:%s healthy=%t status=%q last_error=%q\n",
			key, c.GetHealthy(), c.GetStatus(), c.GetLastError())
	}

	return b.String()
}

func collectHealthInput(cfg configuration.ConfigurationData, start, now time.Time) healthInput {
	probes := make([]probeInput, 0, len(cfg.Probes))
	for _, p := range cfg.Probes {
		probes = append(probes, probeInput{
			Name:    p.Name,
			Enabled: p.IsEnabled(),
			State:   agentstate.GetProbeRunState(p.ID()),
		})
	}

	failures := agentstate.GetStrategyFailures()
	failing := agentstate.FailingExports()

	outputs := make([]outputInput, 0, len(cfg.StorageConfig))
	for _, sc := range cfg.StorageConfig {
		out := outputInput{Name: sc.Name}

		if failure, ok := failures[sc.Name]; ok {
			f := failure
			out.Failure = &f
		}

		out.Failing = slices.Contains(failing, sc.Name)

		out.LastError = latestExportError(agentstate.GetExportActivities(sc.Name))
		outputs = append(outputs, out)
	}

	return healthInput{
		Probes:    probes,
		Outputs:   outputs,
		StartTime: start,
		Now:       now,
	}
}

func latestExportError(activities map[string]agentstate.ExportActivity) string {
	var latest time.Time
	var lastError string
	for _, activity := range activities {
		if activity.LastFailure.After(latest) {
			latest = activity.LastFailure
			lastError = activity.LastError
		}
	}
	return lastError
}
