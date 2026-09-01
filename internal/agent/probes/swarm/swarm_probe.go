// Package swarm implements the free-tier swarm probe: Docker Swarm cluster
// state — nodes and quorum, services and their convergence, tasks and their
// failures, and the overlay networks that carry traffic between them.
//
// It is a separate probe from `docker` rather than an extension of it, and the
// line is the observation scope: `docker` watches the containers of ONE host,
// while everything here is cluster-wide and answered only by a manager. Mixing
// them would put host-scoped and cluster-scoped facts behind one probe name,
// one interval and one entity model — the same blur avoided by keeping
// kubernetes out of `docker`.
//
// Transport is the Engine API over its Unix socket via stdlib net/http, the
// posture the docker probe established: no SDK, no dependency tree.
package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/probes/dockerdial"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier — used in JWT claims, the
// transformer YAML name and DiscriminantTagsRegistry. Renaming it breaks every
// licence already issued that names it.
const ProbeType = "swarm"

const (
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second
	apiVersion      = "v1.43"
)

type probeConfig struct {
	SocketPath string
	Interval   time.Duration
	Timeout    time.Duration
}

type swarmProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client

	// notManagerLogged keeps the "this node is not a manager" explanation to
	// one line instead of one per cycle. The metric still reports every cycle;
	// it is the prose that would become noise.
	notManagerLogged atomic.Bool

	entitySrc *entitySource
	newClient func() *http.Client
}

// NewSwarmProbe builds the probe from its YAML config block.
func NewSwarmProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}
	p := &swarmProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.swarm"),
	}
	p.SetProbeType(ProbeType)
	p.entitySrc = newEntitySource()
	p.SetEntitySource(p.entitySrc)
	p.newClient = p.buildClient
	p.client = p.newClient()
	return p, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		SocketPath: dockerdial.DefaultAddress(),
		Interval:   defaultInterval,
		Timeout:    defaultTimeout,
	}
	if v, ok := config["socket_path"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.SocketPath = strings.TrimSpace(v)
	}
	if v, ok := toSeconds(config["interval"]); ok && v > 0 {
		cfg.Interval = v
	}
	if v, ok := toSeconds(config["timeout"]); ok && v > 0 {
		cfg.Timeout = v
	}
	return cfg, nil
}

// toSeconds accepts the two spellings a YAML loader can produce for a duration
// written as a bare number: int and float64.
func toSeconds(v any) (time.Duration, bool) {
	switch n := v.(type) {
	case int:
		return time.Duration(n) * time.Second, true
	case int64:
		return time.Duration(n) * time.Second, true
	case float64:
		return time.Duration(n) * time.Second, true
	}
	return 0, false
}

func (p *swarmProbe) buildClient() *http.Client {
	socketPath := p.cfg.SocketPath
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dockerdial.Dial(ctx, socketPath)
			},
		},
		Timeout: p.cfg.Timeout,
	}
}

func (p *swarmProbe) GetInterval() time.Duration { return p.cfg.Interval }
func (p *swarmProbe) ShouldStart() bool          { return true }

func (p *swarmProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().Str("socket", p.cfg.SocketPath).Msg("Starting swarm probe")
	return nil
}

func (p *swarmProbe) OnShutdown(ctx context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect gathers one cycle of cluster state.
//
// Every collector is attempted even when an earlier one failed: a manager that
// answers /nodes but not /services describes a partial outage, and reporting
// the half that worked is more useful than dropping the cycle. The availability
// metric is emitted unconditionally so a dashboard can tell "the cluster is
// unreachable" from "the probe stopped running".
func (p *swarmProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	var points []data_store.DataPoint

	info, state, err := p.fetchSwarm()
	points = append(points, p.availabilityPoints(state, now)...)
	if state != stateManager {
		p.explainOnce(state, err)
		// Withdraw the entity: a node that cannot see the cluster must not
		// keep publishing the last snapshot it saw as if it were current.
		p.entitySrc.update("", "", 0, 0, nil)
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}
	p.notManagerLogged.Store(false)

	clusterTags := []tags.Tag{
		{Key: "swarm.cluster.id", Value: info.ID},
	}
	if name := info.Spec.Name; name != "" {
		clusterTags = append(clusterTags, tags.Tag{Key: "swarm.cluster.name", Value: name})
	}

	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// A failed list is NOT an empty cluster. Passing nil through would emit
	// swarm.cluster.nodes=0 and managers=0, which reads as "this swarm has no
	// machines" — a confident, wrong statement, and the same healthy-looking
	// zero the worker case is written to avoid. Emitting nothing leaves the
	// last known value visible with its own staleness, next to up=0.
	nodes, err := p.listNodes()
	record(err)
	if err == nil {
		points = append(points, p.nodePoints(nodes, clusterTags, now)...)
	}

	// Same rule for the rest: a service list that failed to load must not be
	// reported as "no services", and a task list that failed must not turn
	// every service into 0 running replicas — which would read as a total
	// outage caused by the probe's own read error.
	services, servicesErr := p.listServices()
	record(servicesErr)
	tasks, tasksErr := p.listTasks()
	record(tasksErr)
	if servicesErr == nil && tasksErr == nil {
		points = append(points, p.servicePoints(services, tasks, clusterTags, now)...)
		points = append(points, p.taskPoints(services, tasks, clusterTags, now)...)
	}

	networks, networksErr := p.listNetworks()
	record(networksErr)
	if networksErr == nil && servicesErr == nil && tasksErr == nil {
		points = append(points, p.overlayPoints(networks, services, tasks, clusterTags, now)...)
	}

	p.entitySrc.update(info.ID, info.Spec.Name, len(nodes), len(services), segmentFactsOf(networks))

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), firstErr
}

// clusterState distinguishes the three situations that all look like "no data"
// from the outside and mean very different things to an operator.
type clusterState int

const (
	stateManager     clusterState = iota // this node manages the swarm: full view
	stateWorker                          // in a swarm, but not a manager: no view
	stateNotInSwarm                      // engine is up, swarm mode is off
	stateUnreachable                     // the Docker socket itself did not answer
)

func (s clusterState) String() string {
	switch s {
	case stateManager:
		return "manager"
	case stateWorker:
		return "worker"
	case stateNotInSwarm:
		return "not_in_swarm"
	default:
		return "unreachable"
	}
}

// fetchSwarm reads GET /swarm and classifies what this node can see.
//
// The Engine answers 503 both for "not a manager" and for "not in a swarm",
// with the distinction only in the message body. Telling them apart matters:
// one is a probe pointed at the wrong node, the other is a probe pointed at a
// machine that was never clustered. Reporting either as an empty cluster would
// be the worst answer — a healthy-looking zero.
func (p *swarmProbe) fetchSwarm() (swarmInfo, clusterState, error) {
	var info swarmInfo
	body, status, err := p.get("/swarm")
	if err != nil {
		return info, stateUnreachable, err
	}
	if status == http.StatusServiceUnavailable {
		if strings.Contains(strings.ToLower(string(body)), "not a swarm manager") {
			return info, stateWorker, nil
		}
		return info, stateNotInSwarm, nil
	}
	if status != http.StatusOK {
		return info, stateUnreachable, fmt.Errorf("GET /swarm returned %d", status)
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return info, stateUnreachable, fmt.Errorf("decoding /swarm: %w", err)
	}
	return info, stateManager, nil
}

// availabilityPoints emits one series per state, so a dashboard reads the
// reason rather than a bare zero.
func (p *swarmProbe) availabilityPoints(state clusterState, now time.Time) []data_store.DataPoint {
	up := float64(0)
	if state == stateManager {
		up = 1
	}
	points := []data_store.DataPoint{
		point("senhub.swarm.up", up, now, []tags.Tag{{Key: "metric_type", Value: "availability"}}),
	}
	for _, s := range []clusterState{stateManager, stateWorker, stateNotInSwarm, stateUnreachable} {
		v := float64(0)
		if s == state {
			v = 1
		}
		points = append(points, point("senhub.swarm.node_role_state", v, now, []tags.Tag{
			{Key: "state", Value: s.String()},
			{Key: "metric_type", Value: "availability"},
		}))
	}
	return points
}

// explainOnce writes the prose form of a non-manager state a single time.
// The metric repeats every cycle; the sentence would only repeat itself.
func (p *swarmProbe) explainOnce(state clusterState, err error) {
	if p.notManagerLogged.Swap(true) {
		return
	}
	switch state {
	case stateWorker:
		p.moduleLogger.Warn().Msg("swarm: this node is a worker; cluster state is only readable from a manager — point the probe at a manager node")
	case stateNotInSwarm:
		p.moduleLogger.Warn().Msg("swarm: the Docker engine is not in swarm mode; no cluster to report")
	default:
		p.moduleLogger.Error().Err(err).Str("socket", p.cfg.SocketPath).Msg("swarm: the Docker socket did not answer")
	}
}

// get performs one Engine API call and returns the raw body with its status.
// The status is returned rather than folded into an error because several
// callers treat 503 as information, not failure.
func (p *swarmProbe) get(path string) ([]byte, int, error) {
	url := fmt.Sprintf("http://localhost/%s%s", apiVersion, path)
	resp, err := p.client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading %s body: %w", path, err)
	}
	return body, resp.StatusCode, nil
}

// getJSON decodes a list endpoint.
func getJSON[T any](p *swarmProbe, path string) ([]T, error) {
	body, status, err := p.get(path)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %d", path, status)
	}
	var out []T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return out, nil
}

func (p *swarmProbe) listNodes() ([]node, error)       { return getJSON[node](p, "/nodes") }
func (p *swarmProbe) listServices() ([]service, error) { return getJSON[service](p, "/services") }
func (p *swarmProbe) listTasks() ([]task, error)       { return getJSON[task](p, "/tasks") }
func (p *swarmProbe) listNetworks() ([]network, error) { return getJSON[network](p, "/networks") }

// point builds one datapoint. Every metric in this probe is a gauge read from
// a full cluster snapshot, so there is a single shape.
func point(name string, value float64, ts time.Time, t []tags.Tag) data_store.DataPoint {
	return data_store.DataPoint{
		Name:      name,
		Value:     value,
		Timestamp: ts,
		Tags:      t,
	}
}

// withTags concatenates without aliasing the base slice — appending to a shared
// backing array is how two metrics silently end up with each other's labels.
func withTags(base []tags.Tag, extra ...tags.Tag) []tags.Tag {
	out := make([]tags.Tag, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return out
}

// oneHot emits one series per known value of an enumerated field, carrying 0 or
// 1. A state is a string and a string cannot be a metric value; one-hot lets a
// dashboard sum "how many nodes are draining" without decoding an enum, and an
// unknown value upstream lights none of the known series instead of silently
// mapping onto one of them.
func oneHot(name, current string, known []string, ts time.Time, base []tags.Tag, key string) []data_store.DataPoint {
	points := make([]data_store.DataPoint, 0, len(known))
	for _, v := range known {
		f := float64(0)
		if strings.EqualFold(v, current) {
			f = 1
		}
		points = append(points, point(name, f, ts, withTags(base, tags.Tag{Key: key, Value: v})))
	}
	return points
}
