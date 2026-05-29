package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// licenseCollector reports per-product license usage from
// QSYS2.LICENSE_INFO — usage_limit vs usage_count, peak usage,
// grace period, expiration. Critical for sites that pay per-user
// or per-processor-group IBM i licenses: a crossed threshold is
// both a compliance risk and a potential service impact.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-license-info
type licenseCollector struct{}

func (licenseCollector) Name() string  { return "license" }
func (licenseCollector) IsEvent() bool { return false }

func (licenseCollector) SQL() string {
	return "SELECT PRODUCT_ID, LICENSE_TERM, RELEASE_LEVEL, FEATURE_ID, INSTALLED, USAGE_LIMIT, USAGE_COUNT, PEAK_USAGE, LICENSED_USER_COUNT FROM QSYS2.LICENSE_INFO WHERE INSTALLED = 'YES'"
}

func (licenseCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("LICENSE_INFO returned no installed products")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*4+1)

	for _, row := range res.Rows {
		product := trimmedCell(row, idx, "PRODUCT_ID")
		if product == "" {
			continue
		}
		tags := []tags.Tag{
			hostTag,
			{Key: "product_id", Value: product},
			{Key: "feature_id", Value: trimmedCell(row, idx, "FEATURE_ID")},
			{Key: "release_level", Value: trimmedCell(row, idx, "RELEASE_LEVEL")},
			{Key: "license_term", Value: trimmedCell(row, idx, "LICENSE_TERM")},
		}
		metrics := []struct {
			column string
			metric string
		}{
			{"USAGE_LIMIT", "ibmi.license.usage_limit"},
			{"USAGE_COUNT", "ibmi.license.usage_count"},
			{"PEAK_USAGE", "ibmi.license.peak_usage"},
			{"LICENSED_USER_COUNT", "ibmi.license.licensed_user_count"},
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
		Name:      "ibmi.license.products_installed",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})
	return points, nil
}
