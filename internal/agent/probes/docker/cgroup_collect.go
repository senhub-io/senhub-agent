package docker

import (
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Which source produced this cycle's metrics. Published as a one-hot series so
// the mode is visible on a dashboard rather than only in the agent log: the two
// sources do not carry the same fields, and an operator comparing container
// bandwidth across hosts needs to know which of them cannot report it at all.
const (
	sourceSocket = "socket"
	sourceCgroup = "cgroup"
)

var allSources = []string{sourceSocket, sourceCgroup}

// sourcePoints emits the one-hot describing where this cycle's data came from.
func (p *dockerProbe) sourcePoints(active string, ts time.Time) []data_store.DataPoint {
	points := make([]data_store.DataPoint, 0, len(allSources))
	for _, s := range allSources {
		v := float64(0)
		if s == active {
			v = 1
		}
		points = append(points, data_store.DataPoint{
			Name:      "senhub.docker.source",
			Value:     v,
			Timestamp: ts,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "status"},
				{Key: "source", Value: s},
			},
		})
	}
	return points
}

// collectFromCgroups is the unprivileged path, used when the Docker socket
// cannot be reached. It reports ok=false when the fallback cannot run at all,
// so the caller can surface the original socket error instead.
//
// Containers are identified by id only — the name, image and state live behind
// the socket — so the tags are deliberately thin rather than invented. A
// container_name guessed from the cgroup path would be wrong the moment anyone
// renamed a container, and wrong identity is worse than absent identity.
func (p *dockerProbe) collectFromCgroups(ts time.Time, socketErr error) ([]data_store.DataPoint, bool) {
	if p.cgroups == nil {
		return nil, false
	}
	if !p.cgroups.unifiedAvailable() {
		// cgroup v1 splits controllers across sibling trees and needs a
		// different reader. Publishing half of it would be worse than
		// publishing none, so say so and let the socket error stand.
		p.warnOnce("docker: the socket is unreachable and this host uses cgroup v1, " +
			"which the unprivileged fallback does not read; grant access to the socket or see the least-privilege guide")
		return nil, false
	}

	found, err := p.cgroups.discover()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("docker: scanning cgroups failed")
		return nil, false
	}
	if len(found) == 0 {
		// No container cgroups at all. That is a host with no containers, not a
		// permission problem, and reporting an empty cycle is correct.
		return p.sourcePoints(sourceCgroup, ts), true
	}

	p.warnOnce("docker: the socket is unreachable, reading container counters from cgroups instead — " +
		"CPU, memory, block I/O and process counts are reported; per-container network, restart counts " +
		"and container names are not, because they are only available through the socket")

	points := p.sourcePoints(sourceCgroup, ts)
	for id, dir := range found {
		stats, statErr := p.cgroups.stats(dir)
		if statErr != nil {
			p.moduleLogger.Warn().Err(statErr).Str("container_id", id).Msg("docker: reading container cgroup failed")
			continue
		}
		// State is set to running because a cgroup only exists while the
		// container does: an exited container has no cgroup to read, so it is
		// simply absent rather than reported as stopped.
		points = append(points, p.buildDatapoints(statsResult{
			container: containerListItem{ID: id, State: "running"},
			stats:     stats,
		}, ts)...)
	}
	return points, true
}

// warnOnce logs an explanation the first time only. The fallback is a steady
// state on a hardened install, not an incident, so repeating it every cycle
// would train the reader to filter out the one line that explains the missing
// series.
func (p *dockerProbe) warnOnce(msg string) {
	if p.socketWarned {
		return
	}
	p.socketWarned = true
	p.moduleLogger.Warn().Msg(msg)
}
