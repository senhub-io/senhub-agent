package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// ptfGroupCollector reports the installation state of PTF groups via
// QSYS2.GROUP_PTF_INFO. A PTF group is a curated bundle of fixes from
// IBM (SF99775 TCP/IP, SF99704 HIPER, SF99738 Security, …); tracking
// their status is the single most useful observation for patch
// compliance and security posture.
//
// **Opt-in collector.** Reading GROUP_PTF_INFO requires *SERVICE or
// *ALLOBJ special authority, which the PGMR profile on PUB400 does
// not carry — activating it on the sandbox only produces failure_total
// increments. Wired in allKnownCollectors() and gated via
// `enabled_collectors: [ptf_group, ...]` in the probe configuration.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-group-ptf-info
type ptfGroupCollector struct{}

func newPtfGroupCollector() *ptfGroupCollector { return &ptfGroupCollector{} }

func (*ptfGroupCollector) Name() string  { return "ptf_group" }
func (*ptfGroupCollector) IsEvent() bool { return false }

func (*ptfGroupCollector) SQL() string {
	return "SELECT PTF_GROUP_NAME, PTF_GROUP_LEVEL, PTF_GROUP_STATUS, PTF_GROUP_TARGET_RELEASE FROM QSYS2.GROUP_PTF_INFO"
}

func (*ptfGroupCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.ptf_group.total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	// Emit a per-group gauge (level) plus a status-boolean flag and a
	// per-status aggregate counter. The gauge lets dashboards plot
	// compliance drift (level vs target), while the aggregate flags
	// how many groups sit in NOT_INSTALLED or ERROR state at a glance.
	statusCounts := make(map[string]int, 6)
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*2+len(statusCounts)+1)

	for _, row := range res.Rows {
		name := trimmedCell(row, idx, "PTF_GROUP_NAME")
		if name == "" {
			continue
		}
		status := trimmedCell(row, idx, "PTF_GROUP_STATUS")
		if status == "" {
			status = "UNKNOWN"
		}
		target := trimmedCell(row, idx, "PTF_GROUP_TARGET_RELEASE")
		statusCounts[status]++

		tags := []tags.Tag{
			hostTag,
			{Key: "group", Value: name},
			{Key: "status", Value: status},
			{Key: "target_release", Value: target},
		}

		if v, ok := parseFloatCell(row, idx, "PTF_GROUP_LEVEL"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.ptf_group.level",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}

		// Compliance boolean: 1 if INSTALLED or NOT_APPLICABLE, 0
		// otherwise. Dashboards can build a compliance ratio from
		// this series without parsing strings.
		var installed float32
		if strings.EqualFold(status, "INSTALLED") || strings.EqualFold(status, "NOT_APPLICABLE") {
			installed = 1
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.ptf_group.installed",
			Timestamp: ts,
			Value:     installed,
			Tags:      tags,
		})
	}

	for status, count := range statusCounts {
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.ptf_group.count_by_status",
			Timestamp: ts,
			Value:     float32(count),
			Tags:      []tags.Tag{hostTag, {Key: "status", Value: status}},
		})
	}
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.ptf_group.total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("GROUP_PTF_INFO produced no usable datapoints")
	}
	return points, nil
}

// ptfCollector aggregates individual PTF entries from QSYS2.PTF_INFO
// by loaded status. Individual PTFs are too numerous (often >1000) to
// surface per-row, so the SQL does server-side GROUP BY on
// PTF_LOADED_STATUS and emits one count metric per status bucket.
//
// **Opt-in collector.** Same authority requirement as ptf_group —
// PGMR-level profiles on PUB400 cannot read PTF_INFO.
type ptfCollector struct{}

func newPtfCollector() *ptfCollector { return &ptfCollector{} }

func (*ptfCollector) Name() string  { return "ptf" }
func (*ptfCollector) IsEvent() bool { return false }

func (*ptfCollector) SQL() string {
	return "SELECT PTF_LOADED_STATUS, COUNT(*) AS PTF_COUNT FROM QSYS2.PTF_INFO GROUP BY PTF_LOADED_STATUS"
}

func (*ptfCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.ptf.total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)+1)
	var total float64

	for _, row := range res.Rows {
		status := trimmedCell(row, idx, "PTF_LOADED_STATUS")
		if status == "" {
			status = "UNKNOWN"
		}
		v, ok := parseFloatCell(row, idx, "PTF_COUNT")
		if !ok {
			continue
		}
		total += v
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.ptf.count_by_status",
			Timestamp: ts,
			Value:     float32(v),
			Tags:      []tags.Tag{hostTag, {Key: "status", Value: status}},
		})
	}
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.ptf.total",
		Timestamp: ts,
		Value:     float32(total),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("PTF_INFO produced no usable datapoints")
	}
	return points, nil
}
