package ibmi

import (
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// mediaLibraryCollector reports the state of tape/media libraries from
// QSYS2.MEDIA_LIBRARY_INFO. Zero rows is a valid state (no tape
// hardware attached — common on virtualised IBM i); we still emit a
// zero aggregate so the time series stays continuous and an operator
// can assert "no tape devices on this LPAR".
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-media-library-info
type mediaLibraryCollector struct{}

func (mediaLibraryCollector) Name() string  { return "media_library" }
func (mediaLibraryCollector) IsEvent() bool { return false }

func (mediaLibraryCollector) SQL() string {
	return "SELECT DEVICE_NAME, DEVICE_STATUS, DEVICE_TYPE, DEVICE_MODEL, RESOURCE_NAME, RESOURCE_STATUS, RESOURCE_ALLOCATION_STATUS FROM QSYS2.MEDIA_LIBRARY_INFO"
}

func (mediaLibraryCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	hostTag := tags.Tag{Key: "host", Value: host}
	points := make([]datapoint.DataPoint, 0, len(res.Rows)+1)

	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name: "ibmi.media_library.devices_total", Timestamp: ts,
			Value: 0, Tags: []tags.Tag{hostTag},
		}}, nil
	}

	idx := columnIndex(res.Columns)
	for _, row := range res.Rows {
		name := trimmedCell(row, idx, "DEVICE_NAME")
		if name == "" {
			continue
		}
		status := trimmedCell(row, idx, "DEVICE_STATUS")
		var value float32
		if status == "VARIED ON" || status == "ACTIVE" {
			value = 1
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.media_library.device_up",
			Timestamp: ts,
			Value:     value,
			Tags: []tags.Tag{
				hostTag,
				{Key: "device_name", Value: name},
				{Key: "device_type", Value: trimmedCell(row, idx, "DEVICE_TYPE")},
				{Key: "device_model", Value: trimmedCell(row, idx, "DEVICE_MODEL")},
				{Key: "status", Value: status},
			},
		})
	}
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.media_library.devices_total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})
	return points, nil
}
