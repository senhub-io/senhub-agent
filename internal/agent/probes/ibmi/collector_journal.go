package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// journalInfoCollector reads the live set of active journals from
// QSYS2.JOURNAL_INFO. Every row is a journal definition with the
// attached receiver and various lag indicators. Cardinality is
// limited by the number of journaled libraries/databases — in
// practice a few dozen journals on a typical system.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-journal-info
type journalInfoCollector struct{}

func (journalInfoCollector) Name() string  { return "journal_info" }
func (journalInfoCollector) IsEvent() bool { return false }

func (journalInfoCollector) SQL() string {
	// ESTIMATED_TIME_BEHIND and MAXIMUM_TIME_BEHIND are populated
	// only when NUMBER_REMOTE_JOURNALS > 0 — they quantify the lag
	// between the source journal and its remote replicas. On LPARs
	// without remote journaling (e.g. PUB400) every row returns NULL
	// for those columns, which Parse handles gracefully.
	return "SELECT JOURNAL_NAME, JOURNAL_LIBRARY, JOURNAL_STATE, JOURNAL_TYPE, NUMBER_JOURNAL_RECEIVERS, TOTAL_SIZE_JOURNAL_RECEIVERS, NUMBER_REMOTE_JOURNALS, ESTIMATED_TIME_BEHIND, MAXIMUM_TIME_BEHIND FROM QSYS2.JOURNAL_INFO"
}

func (journalInfoCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.journal.total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{{Key: "host", Value: host}},
		}}, nil
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}
	stateCounts := make(map[string]int, 3)
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*3+5)

	for _, row := range res.Rows {
		name := trimmedCell(row, idx, "JOURNAL_NAME")
		if name == "" {
			continue
		}
		library := trimmedCell(row, idx, "JOURNAL_LIBRARY")
		state := trimmedCell(row, idx, "JOURNAL_STATE")
		jtype := trimmedCell(row, idx, "JOURNAL_TYPE")
		stateCounts[state]++

		tags := []tags.Tag{
			hostTag,
			{Key: "journal", Value: name},
			{Key: "journal_library", Value: library},
			{Key: "journal_type", Value: jtype},
			{Key: "state", Value: state},
		}

		var stateValue float32
		if strings.TrimSpace(state) == "*ACTIVE" {
			stateValue = 1
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.journal.active",
			Timestamp: ts,
			Value:     stateValue,
			Tags:      tags,
		})
		if v, ok := parseFloatCell(row, idx, "NUMBER_JOURNAL_RECEIVERS"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.journal.receivers_count",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		if v, ok := parseFloatCell(row, idx, "TOTAL_SIZE_JOURNAL_RECEIVERS"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.journal.receivers_total_size_kb",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		if v, ok := parseFloatCell(row, idx, "NUMBER_REMOTE_JOURNALS"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.journal.remote_journals_count",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		// Remote-journal lag: these two columns come back as a
		// numeric (seconds) on LPARs that have remote journaling
		// configured and NULL otherwise. We emit the metric only
		// when the SQL Service returned a real value so empty
		// LPARs don't pollute dashboards with constant zeros.
		if v, ok := parseFloatCell(row, idx, "ESTIMATED_TIME_BEHIND"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.journal.remote_lag_estimated_seconds",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		if v, ok := parseFloatCell(row, idx, "MAXIMUM_TIME_BEHIND"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.journal.remote_lag_maximum_seconds",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
	}

	for state, count := range stateCounts {
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.journal.count_by_state",
			Timestamp: ts,
			Value:     float32(count),
			Tags:      []tags.Tag{hostTag, {Key: "state", Value: state}},
		})
	}
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.journal.total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("journal_info produced no datapoints")
	}
	return points, nil
}

// journalReceiverCollector reads JOURNAL_RECEIVER_INFO for receivers
// currently ATTACHED (the active receivers being written to). Reports
// their size, number of entries and pending transaction count —
// metrics that matter for detecting journal-full conditions before
// they take down a subsystem.
type journalReceiverCollector struct{}

func (journalReceiverCollector) Name() string  { return "journal_receiver" }
func (journalReceiverCollector) IsEvent() bool { return false }

func (journalReceiverCollector) SQL() string {
	return "SELECT JOURNAL_RECEIVER_NAME, JOURNAL_RECEIVER_LIBRARY, JOURNAL_LIBRARY, JOURNAL_NAME, STATUS, SIZE, THRESHOLD, NUMBER_OF_JOURNAL_ENTRIES, PENDING_TRANSACTIONS FROM QSYS2.JOURNAL_RECEIVER_INFO WHERE STATUS = 'ATTACHED'"
}

func (journalReceiverCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.journal_receiver.attached_total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{{Key: "host", Value: host}},
		}}, nil
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*4+1)

	for _, row := range res.Rows {
		name := trimmedCell(row, idx, "JOURNAL_RECEIVER_NAME")
		if name == "" {
			continue
		}
		tags := []tags.Tag{
			hostTag,
			{Key: "receiver", Value: name},
			{Key: "receiver_library", Value: trimmedCell(row, idx, "JOURNAL_RECEIVER_LIBRARY")},
			{Key: "journal", Value: trimmedCell(row, idx, "JOURNAL_NAME")},
			{Key: "journal_library", Value: trimmedCell(row, idx, "JOURNAL_LIBRARY")},
		}
		metrics := []struct {
			column string
			metric string
		}{
			{"SIZE", "ibmi.journal_receiver.size_kb"},
			{"THRESHOLD", "ibmi.journal_receiver.threshold_kb"},
			{"NUMBER_OF_JOURNAL_ENTRIES", "ibmi.journal_receiver.entries_count"},
			{"PENDING_TRANSACTIONS", "ibmi.journal_receiver.pending_transactions"},
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
		Name:      "ibmi.journal_receiver.attached_total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})
	return points, nil
}
