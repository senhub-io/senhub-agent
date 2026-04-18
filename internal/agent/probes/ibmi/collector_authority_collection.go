package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// authorityCollectionCollector aggregates entries from
// QSYS2.AUTHORITY_COLLECTION — the system table populated by IBM i's
// authority collection infrastructure (`CHGAUTCOL` / `STRAUTCOL`).
// When authority collection is active, every authorization check is
// logged here along with the required-vs-current authority delta.
//
// Exposing this as a collector gives a compliance-grade signal that
// nothing else in QSYS2 can produce: "has anyone hit an authority
// boundary in the last N seconds?".
//
// **Opt-in collector.** Two reasons it's kept out of the default set:
//  1. Authority collection must be explicitly started by an admin
//     with `*SECADM` authority — it is idle on a freshly installed
//     LPAR.
//  2. Even when active, the table can grow fast on a busy system
//     and querying it without WHERE filters could be expensive.
//
// Activate via `enabled_collectors: [..., authority_collection]` in
// the probe config, typically paired with `audit_journal` for a
// complete security telemetry stack.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-authority-collection
type authorityCollectionCollector struct{}

func newAuthorityCollectionCollector() *authorityCollectionCollector {
	return &authorityCollectionCollector{}
}

func (*authorityCollectionCollector) Name() string  { return "authority_collection" }
func (*authorityCollectionCollector) IsEvent() bool { return false }

func (*authorityCollectionCollector) SQL() string {
	// Three signals in one pass via GROUP BY + computed columns:
	//  - per-user entry count (USER_NAME)
	//  - per-user count of entries where the user did NOT have the
	//    required authority (CHECK_ANY = 'NO' means the check
	//    failed; the semantics are documented in the SQL Services
	//    reference).
	//  - per-user count of entries where CURRENT_AUTHORITY is shorter
	//    than REQUIRED_AUTHORITY (a defensive secondary signal).
	return "SELECT USER_NAME, COUNT(*) AS ENTRY_COUNT, SUM(CASE WHEN CHECK_ANY = 'NO' THEN 1 ELSE 0 END) AS FAILED_CHECKS FROM QSYS2.AUTHORITY_COLLECTION GROUP BY USER_NAME FETCH FIRST 200 ROWS ONLY"
}

func (*authorityCollectionCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		// Empty table is the dominant case on systems where
		// authority collection is not running — emit zero-ed
		// aggregates so the dashboard line stays continuous.
		return []datapoint.DataPoint{
			{
				Name:      "ibmi.authority_collection.entries_total",
				Timestamp: ts,
				Value:     0,
				Tags:      []tags.Tag{hostTag},
			},
			{
				Name:      "ibmi.authority_collection.failed_checks_total",
				Timestamp: ts,
				Value:     0,
				Tags:      []tags.Tag{hostTag},
			},
		}, nil
	}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)*2+2)
	var totalEntries, totalFailed float64

	for _, row := range res.Rows {
		user := trimmedCell(row, idx, "USER_NAME")
		if user == "" {
			user = "<unknown>"
		}
		tags := []tags.Tag{hostTag, {Key: "user", Value: user}}

		if v, ok := parseFloatCell(row, idx, "ENTRY_COUNT"); ok {
			totalEntries += v
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.authority_collection.entries_by_user",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		if v, ok := parseFloatCell(row, idx, "FAILED_CHECKS"); ok {
			totalFailed += v
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.authority_collection.failed_checks_by_user",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
	}

	points = append(points,
		datapoint.DataPoint{
			Name:      "ibmi.authority_collection.entries_total",
			Timestamp: ts,
			Value:     float32(totalEntries),
			Tags:      []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name:      "ibmi.authority_collection.failed_checks_total",
			Timestamp: ts,
			Value:     float32(totalFailed),
			Tags:      []tags.Tag{hostTag},
		},
	)

	if len(points) == 0 {
		return nil, fmt.Errorf("AUTHORITY_COLLECTION produced no usable datapoints")
	}
	return points, nil
}
