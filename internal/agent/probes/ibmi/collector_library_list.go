package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// libraryListCollector reports the current library list of the JDBC
// connection as seen from QSYS2.LIBRARY_LIST_INFO. It is not
// operational in the classic sense (the list is snapshotted at
// connection time and doesn't vary across cycles), but it is a useful
// configuration-audit data point: a deployment can assert that the
// bridge's library list matches what it expects.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-library-list-info
type libraryListCollector struct{}

func (libraryListCollector) Name() string  { return "library_list" }
func (libraryListCollector) IsEvent() bool { return false }

func (libraryListCollector) SQL() string {
	return "SELECT ORDINAL_POSITION, SCHEMA_NAME, TYPE, IASP_NUMBER FROM QSYS2.LIBRARY_LIST_INFO ORDER BY ORDINAL_POSITION"
}

func (libraryListCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("LIBRARY_LIST_INFO returned no rows")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}
	typeCounts := make(map[string]int, 4)

	points := make([]datapoint.DataPoint, 0, len(res.Rows)+3)
	for _, row := range res.Rows {
		schema := trimmedCell(row, idx, "SCHEMA_NAME")
		if schema == "" {
			continue
		}
		libType := trimmedCell(row, idx, "TYPE")
		typeCounts[libType]++
		position := trimmedCell(row, idx, "ORDINAL_POSITION")

		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.library_list.position",
			Timestamp: ts,
			Value:     1,
			Tags: []tags.Tag{
				hostTag,
				{Key: "schema", Value: schema},
				{Key: "type", Value: libType},
				{Key: "position", Value: position},
			},
		})
	}
	for libType, count := range typeCounts {
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.library_list.count_by_type",
			Timestamp: ts,
			Value:     float32(count),
			Tags:      []tags.Tag{hostTag, {Key: "type", Value: libType}},
		})
	}
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.library_list.total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})
	return points, nil
}
