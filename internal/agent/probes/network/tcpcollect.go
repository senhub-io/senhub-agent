package network

import (
	"sort"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity/hostdep"
	"senhub-agent.go/internal/agent/services/netconn"
	"senhub-agent.go/internal/agent/tags"
)

// tcpSource abstracts platform-specific TCP data providers.
// A nil field means the corresponding data is not available on this platform.
type tcpSource struct {
	stats func() (TCPStats, error)
	conns func() ([]netconn.Conn, error)
	// edges derives the per-edge connection metrics from the socket table read
	// for the state counts; nil disables them.
	edges func([]netconn.Conn) []hostdep.EdgeMetric
}

// hostTCPDataPoints collects all host-wide TCP metrics from the given source.
// It never modifies the base tags slice: each point receives a fresh copy.
func hostTCPDataPoints(src tcpSource, base []tags.Tag, ts time.Time) ([]data_store.DataPoint, []error) {
	var points []data_store.DataPoint
	var errs []error

	// Connections by state
	if src.conns != nil {
		conns, err := src.conns()
		if err != nil {
			errs = append(errs, err)
		} else {
			counts := countByState(conns)
			states := make([]string, 0, len(counts))
			for state := range counts {
				states = append(states, state)
			}
			sort.Strings(states)
			for _, state := range states {
				pointTags := append([]tags.Tag{}, base...)
				pointTags = append(pointTags, tags.Tag{Key: "state", Value: state})
				points = append(points, data_store.DataPoint{
					Name:      "tcp_connections",
					Timestamp: ts,
					Value:     float64(counts[state]),
					Tags:      pointTags,
				})
			}
			if src.edges != nil {
				points = append(points, edgeDataPoints(src.edges(conns), base, ts)...)
			}
		}
	}

	// Host-wide TCP counters
	if src.stats != nil {
		stats, err := src.stats()
		if err != nil {
			errs = append(errs, err)
		} else {
			counters := []struct {
				name  string
				value float64
			}{
				{"tcp_active_opens", float64(stats.ActiveOpens)},
				{"tcp_passive_opens", float64(stats.PassiveOpens)},
				{"tcp_attempt_fails", float64(stats.AttemptFails)},
				{"tcp_estab_resets", float64(stats.EstabResets)},
				{"tcp_retrans_segs", float64(stats.RetransSegs)},
				{"tcp_out_rsts", float64(stats.OutRsts)},
			}
			for _, c := range counters {
				pointTags := append([]tags.Tag{}, base...)
				points = append(points, data_store.DataPoint{
					Name:      c.name,
					Timestamp: ts,
					Value:     c.value,
					Tags:      pointTags,
				})
			}
			if stats.HasListenOverflows {
				pointTags := append([]tags.Tag{}, base...)
				points = append(points, data_store.DataPoint{
					Name:      "tcp_listen_overflows",
					Timestamp: ts,
					Value:     float64(stats.ListenOverflows),
					Tags:      pointTags,
				})
			}
		}
	}

	return points, errs
}

// edgeDataPoints renders per-edge metrics. The tags are exactly the keys of the
// depends_on edge, after the host tags; the client port never appears.
func edgeDataPoints(edges []hostdep.EdgeMetric, base []tags.Tag, ts time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint
	for _, e := range edges {
		keys := make([]string, 0, len(e.Attributes))
		for k := range e.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		add := func(name string, value float64) {
			pointTags := append([]tags.Tag{}, base...)
			for _, k := range keys {
				pointTags = append(pointTags, tags.Tag{Key: k, Value: e.Attributes[k]})
			}
			points = append(points, data_store.DataPoint{Name: name, Timestamp: ts, Value: value, Tags: pointTags})
		}
		if e.HasRTT {
			add("edge_rtt", e.RTT.Seconds())
		}
		add("edge_retransmits", float64(e.Retransmits))
		add("edge_bytes_sent", float64(e.BytesSent))
		add("edge_bytes_received", float64(e.BytesReceived))
	}
	return points
}

// defaultTCPSource returns the appropriate tcpSource for the current platform.
func defaultTCPSource() tcpSource {
	return tcpSource{stats: readTCPStatsFunc, conns: tcpConnsFunc, edges: hostdep.CollectEdgeMetrics}
}
