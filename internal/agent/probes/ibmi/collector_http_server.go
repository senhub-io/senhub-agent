package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// httpServerCollector reports per-HTTP-instance stats from
// QSYS2.HTTP_SERVER_INFO. IBM i HTTP Server (powered by Apache) is the
// backbone of every IBM i web application — monitoring its health is a
// standard ask from admins.
//
// The view returns multiple rows per server (one per HTTP_FUNCTION
// breakdown). For the POC we scope to the "SERVER HANDLED" function
// which carries the roll-up counters for the whole instance: total
// requests, responses, errors, processing time.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-http-server-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type httpServerCollector struct{}

func (httpServerCollector) Name() string  { return "http_server" }
func (httpServerCollector) IsEvent() bool { return false }

func (httpServerCollector) SQL() string {
	return "SELECT SERVER_NAME, JOB_NAME, SERVER_NORMAL_CONNECTIONS, SERVER_SSL_CONNECTIONS, SERVER_ACTIVE_THREADS, SERVER_IDLE_THREADS, SERVER_TOTAL_REQUESTS, SERVER_TOTAL_REQUESTS_REJECTED, SERVER_TOTAL_RESPONSES FROM QSYS2.HTTP_SERVER_INFO WHERE HTTP_FUNCTION = 'SERVER HANDLED'"
}

func (httpServerCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		// No active HTTP instances is a valid state — emit a single
		// zero aggregate so the time series stays continuous.
		return []datapoint.DataPoint{{
			Name:      "ibmi.http_server.instances_total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{{Key: "host", Value: host}},
		}}, nil
	}

	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*7+1)

	for _, row := range res.Rows {
		serverName := trimmedCell(row, idx, "SERVER_NAME")
		if serverName == "" {
			continue
		}
		jobName := trimmedCell(row, idx, "JOB_NAME")
		tags := []tags.Tag{
			hostTag,
			{Key: "server_name", Value: serverName},
			{Key: "job_name", Value: jobName},
		}

		metrics := []struct {
			column string
			metric string
		}{
			{"SERVER_NORMAL_CONNECTIONS", "ibmi.http_server.normal_connections"},
			{"SERVER_SSL_CONNECTIONS", "ibmi.http_server.ssl_connections"},
			{"SERVER_ACTIVE_THREADS", "ibmi.http_server.active_threads"},
			{"SERVER_IDLE_THREADS", "ibmi.http_server.idle_threads"},
			{"SERVER_TOTAL_REQUESTS", "ibmi.http_server.total_requests"},
			{"SERVER_TOTAL_REQUESTS_REJECTED", "ibmi.http_server.rejected_requests"},
			{"SERVER_TOTAL_RESPONSES", "ibmi.http_server.total_responses"},
		}
		for _, m := range metrics {
			v, ok := parseFloatCell(row, idx, m.column)
			if !ok {
				continue
			}
			points = append(points, datapoint.DataPoint{
				Name:      m.metric,
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
	}

	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.http_server.instances_total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("http_server produced no datapoints")
	}
	return points, nil
}
