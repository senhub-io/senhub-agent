package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// netstatListenerCollector reads listening sockets from QSYS2.NETSTAT_INFO.
// Only rows in TCP_STATE='LISTEN' are fetched, which keeps the result set
// bounded to the active service ports of the LPAR (typically 20–50 rows).
//
// The collector also extracts the **global** TCP counters that NETSTAT_INFO
// replicates on every row (same values everywhere) and emits them as
// single-point aggregates: established connections, active/passive/failed
// opens, segments sent/received/retransmitted/reset. Those counters are
// the closest equivalent to "netstat -s" on Unix.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-netstat-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type netstatListenerCollector struct{}

func (netstatListenerCollector) Name() string  { return "netstat_listener" }
func (netstatListenerCollector) IsEvent() bool { return false }

func (netstatListenerCollector) SQL() string {
	return "SELECT CONNECTION_TYPE, LOCAL_ADDRESS, LOCAL_PORT, LOCAL_PORT_NAME, PROTOCOL, TCP_STATE, BIND_USER, NUMBER_OF_ASSOCIATED_JOBS, TCP_CONNECTIONS_CURRENTLY_ESTABLISHED, TCP_ACTIVE_OPENS, TCP_PASSIVE_OPENS, TCP_FAILED_OPENS, TCP_ESTABLISHED_AND_THEN_RESET, TCP_SEGMENTS_SENT, TCP_SEGMENTS_RECEIVED, TCP_SEGMENTS_RETRANSMITTED, TCP_SEGMENTS_RESET FROM QSYS2.NETSTAT_INFO WHERE TCP_STATE = 'LISTEN'"
}

func (netstatListenerCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("netstat_listener returned no listening sockets")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)+10)

	// Per-listener rows: one gauge = 1 per listening socket,
	// tagged with its identity. Lets a dashboard plot "is port
	// 22 listening?" as a simple presence gauge.
	for _, row := range res.Rows {
		port := trimmedCell(row, idx, "LOCAL_PORT")
		portName := trimmedCell(row, idx, "LOCAL_PORT_NAME")
		if portName == "" {
			portName = "<unnamed>"
		}
		protocol := trimmedCell(row, idx, "PROTOCOL")
		bindUser := trimmedCell(row, idx, "BIND_USER")
		connectionType := trimmedCell(row, idx, "CONNECTION_TYPE")

		tags := []tags.Tag{
			hostTag,
			{Key: "local_port", Value: port},
			{Key: "port_name", Value: portName},
			{Key: "protocol", Value: protocol},
			{Key: "bind_user", Value: bindUser},
			{Key: "connection_type", Value: connectionType},
		}

		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.netstat.listener_up",
			Timestamp: ts,
			Value:     1, // presence gauge
			Tags:      tags,
		})
		if assoc, ok := parseFloatCell(row, idx, "NUMBER_OF_ASSOCIATED_JOBS"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.netstat.listener_jobs",
				Timestamp: ts,
				Value:     float32(assoc),
				Tags:      tags,
			})
		}
	}

	// Global TCP counters come from the first row (they are identical
	// across every row by design of QSYS2.NETSTAT_INFO).
	firstRow := res.Rows[0]
	aggregates := []struct {
		column string
		metric string
	}{
		{"TCP_CONNECTIONS_CURRENTLY_ESTABLISHED", "ibmi.tcp.connections_established"},
		{"TCP_ACTIVE_OPENS", "ibmi.tcp.active_opens_total"},
		{"TCP_PASSIVE_OPENS", "ibmi.tcp.passive_opens_total"},
		{"TCP_FAILED_OPENS", "ibmi.tcp.failed_opens_total"},
		{"TCP_ESTABLISHED_AND_THEN_RESET", "ibmi.tcp.established_reset_total"},
		{"TCP_SEGMENTS_SENT", "ibmi.tcp.segments_sent_total"},
		{"TCP_SEGMENTS_RECEIVED", "ibmi.tcp.segments_received_total"},
		{"TCP_SEGMENTS_RETRANSMITTED", "ibmi.tcp.segments_retransmitted_total"},
		{"TCP_SEGMENTS_RESET", "ibmi.tcp.segments_reset_total"},
	}
	for _, a := range aggregates {
		v, ok := parseFloatCell(firstRow, idx, a.column)
		if !ok {
			continue
		}
		points = append(points, datapoint.DataPoint{
			Name:      a.metric,
			Timestamp: ts,
			Value:     float32(v),
			Tags:      []tags.Tag{hostTag},
		})
	}

	// Listener count aggregate.
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.netstat.listener_total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})

	return points, nil
}

// netstatInterfaceCollector reports per-TCP/IP interface state from
// QSYS2.NETSTAT_INTERFACE_INFO. Emits a presence gauge per interface
// (1 = ACTIVE, 0 = INACTIVE, -1 = OTHER) tagged with the line description
// and the internet address.
type netstatInterfaceCollector struct{}

func (netstatInterfaceCollector) Name() string  { return "netstat_interface" }
func (netstatInterfaceCollector) IsEvent() bool { return false }

func (netstatInterfaceCollector) SQL() string {
	return "SELECT CONNECTION_TYPE, INTERNET_ADDRESS, LINE_DESCRIPTION, INTERFACE_LINE_TYPE, INTERFACE_STATUS, MAXIMUM_TRANSMISSION_UNIT FROM QSYS2.NETSTAT_INTERFACE_INFO"
}

func (netstatInterfaceCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("netstat_interface returned no rows")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	statusCounts := make(map[string]int, 4)
	points := make([]datapoint.DataPoint, 0, len(res.Rows)+3)

	for _, row := range res.Rows {
		addr := trimmedCell(row, idx, "INTERNET_ADDRESS")
		if addr == "" {
			continue
		}
		status := trimmedCell(row, idx, "INTERFACE_STATUS")
		statusCounts[status]++

		tags := []tags.Tag{
			hostTag,
			{Key: "address", Value: addr},
			{Key: "line_description", Value: trimmedCell(row, idx, "LINE_DESCRIPTION")},
			{Key: "line_type", Value: trimmedCell(row, idx, "INTERFACE_LINE_TYPE")},
			{Key: "connection_type", Value: trimmedCell(row, idx, "CONNECTION_TYPE")},
			{Key: "status", Value: status},
		}

		var value float32 = -1
		switch strings.TrimSpace(status) {
		case "ACTIVE":
			value = 1
		case "INACTIVE":
			value = 0
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.netstat.interface_up",
			Timestamp: ts,
			Value:     value,
			Tags:      tags,
		})
		if mtu, ok := parseFloatCell(row, idx, "MAXIMUM_TRANSMISSION_UNIT"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.netstat.interface_mtu",
				Timestamp: ts,
				Value:     float32(mtu),
				Tags:      tags,
			})
		}
	}

	for status, count := range statusCounts {
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.netstat.interface_count_by_status",
			Timestamp: ts,
			Value:     float32(count),
			Tags:      []tags.Tag{hostTag, {Key: "status", Value: status}},
		})
	}
	return points, nil
}

// netstatConnectionCollector reads QSYS2.NETSTAT_INFO aggregated by
// TCP_STATE so we can plot the shape of the connection table over
// time without emitting one datapoint per connection (NETSTAT_INFO
// can return thousands of rows on a busy server). The server-side
// GROUP BY bounds the cardinality to ~10 states (ESTABLISHED,
// TIME_WAIT, CLOSE_WAIT, LISTEN, SYN_SENT, ...) + NULL.
//
// This collector is deliberately complementary to netstat_listener:
// the listener view cares about "which ports are open", this view
// cares about "how many connections are in each lifecycle state" —
// a classic health signal (spikes in CLOSE_WAIT or TIME_WAIT usually
// flag an application bug or a connection leak).
type netstatConnectionCollector struct{}

func (netstatConnectionCollector) Name() string  { return "netstat_connection" }
func (netstatConnectionCollector) IsEvent() bool { return false }

func (netstatConnectionCollector) SQL() string {
	return "SELECT TCP_STATE, COUNT(*) AS CONN_COUNT FROM QSYS2.NETSTAT_INFO GROUP BY TCP_STATE"
}

func (netstatConnectionCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.netstat.connections_total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)+1)
	var total float64

	for _, row := range res.Rows {
		state := trimmedCell(row, idx, "TCP_STATE")
		if state == "" {
			state = "<unknown>"
		}
		v, ok := parseFloatCell(row, idx, "CONN_COUNT")
		if !ok {
			continue
		}
		total += v
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.netstat.connections_by_state",
			Timestamp: ts,
			Value:     float32(v),
			Tags:      []tags.Tag{hostTag, {Key: "tcp_state", Value: state}},
		})
	}
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.netstat.connections_total",
		Timestamp: ts,
		Value:     float32(total),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("netstat_connection produced no usable datapoints")
	}
	return points, nil
}
