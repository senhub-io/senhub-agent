package app

import (
	"context"
	"fmt"
	"io"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	httpstrategy "senhub-agent.go/internal/agent/services/data_store/strategies/http"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

// wireProbeHooks gives the HTTP strategy the two things the configurator
// needs from the probe registry, which the strategy cannot import: build
// a probe to hear what it refuses, and run one collect cycle. Wired once
// at start-up, from the application, which sits above both.
func wireProbeHooks() {
	httpstrategy.ProbeChecker = checkProbeConfig
	httpstrategy.ProbeCollector = collectProbeOnce
}

// quietProbeLogger keeps a probe built for a check from writing into the
// agent's log as if it were running.
func quietProbeLogger() *agentLogger.Logger {
	discard := zerolog.New(io.Discard)
	return (*agentLogger.Logger)(&discard)
}

// checkProbeConfig builds the probe and drops it, the same probe that
// config check runs: no Start, so nothing opens a socket or a file.
func checkProbeConfig(probeType string, params map[string]interface{}) ([]httpstrategy.ProbeIssue, error) {
	ctor, known := probes.LookupProbeConstructor(probeType)
	if !known {
		return nil, fmt.Errorf("probe type %q is not built into this agent", probeType)
	}
	var ctorErr error
	issues := types.CollectParamIssues(func() {
		_, ctorErr = ctor(params, quietProbeLogger())
	})
	out := make([]httpstrategy.ProbeIssue, 0, len(issues))
	for _, is := range issues {
		out = append(out, httpstrategy.ProbeIssue{Key: is.Key, Want: is.Want, Got: is.Got})
	}
	return out, ctorErr
}

// collectProbeOnce builds, starts, collects once and stops a probe, so
// the console can show what a configuration would produce before it is
// saved. The context bounds the attempt; a probe that ignores it is cut
// off by the caller's deadline on the HTTP side.
func collectProbeOnce(ctx context.Context, probeType string, params map[string]interface{}) ([]httpstrategy.PreviewMetric, error) {
	ctor, known := probes.LookupProbeConstructor(probeType)
	if !known {
		return nil, fmt.Errorf("probe type %q is not built into this agent", probeType)
	}
	probe, err := ctor(params, quietProbeLogger())
	if err != nil {
		return nil, err
	}
	quit := make(chan struct{})
	if err := probe.OnStart(quit); err != nil {
		return nil, fmt.Errorf("starting the probe: %w", err)
	}
	defer func() {
		close(quit)
		_ = probe.OnShutdown(context.Background())
	}()

	type outcome struct {
		metrics []httpstrategy.PreviewMetric
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		points, err := probe.Collect()
		if err != nil {
			done <- outcome{err: err}
			return
		}
		out := make([]httpstrategy.PreviewMetric, 0, len(points))
		for _, dp := range points {
			tags := make(map[string]string, len(dp.Tags))
			for _, t := range dp.Tags {
				if !t.Private {
					tags[t.Key] = t.Value
				}
			}
			out = append(out, httpstrategy.PreviewMetric{Name: dp.Name, Value: dp.Value, Tags: tags, Timestamp: dp.Timestamp.Unix()})
		}
		done <- outcome{metrics: out}
	}()
	select {
	case o := <-done:
		return o.metrics, o.err
	case <-ctx.Done():
		return nil, fmt.Errorf("the probe did not answer within the test budget")
	}
}
