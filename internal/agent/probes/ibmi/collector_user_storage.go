package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// userStorageCollector reports per-profile storage consumption and
// quota from QSYS2.USER_STORAGE. One row per (user, ASP) pair — a
// profile can hold objects in multiple ASPs — so both dimensions are
// carried as tags to preserve uniqueness.
//
// MAXIMUM_STORAGE_ALLOWED is either a KB integer or the sentinel
// string "*NOMAX". Only numeric values are emitted as quota metrics;
// profiles without a quota contribute to ibmi.user_storage.used_kb
// only.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-user-storage
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-17).
type userStorageCollector struct{}

func (userStorageCollector) Name() string  { return "user_storage" }
func (userStorageCollector) IsEvent() bool { return false }

func (userStorageCollector) SQL() string {
	// WHERE STORAGE_USED > 0 drops empty profiles and the 200-row
	// cap bounds cardinality on large LPARs (a shared system can
	// easily carry 2000+ profiles). Top-200-by-usage is the right
	// slice for operational monitoring: any user near a quota or
	// running away with storage will be in that set.
	return "SELECT AUTHORIZATION_NAME, ASPGRP, MAXIMUM_STORAGE_ALLOWED, STORAGE_USED FROM QSYS2.USER_STORAGE WHERE STORAGE_USED > 0 ORDER BY STORAGE_USED DESC FETCH FIRST 200 ROWS ONLY"
}

func (userStorageCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.user_storage.users_total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)*3+3)
	var totalUsedKB float64
	var usersWithQuota int
	var usersOver80Pct int

	for _, row := range res.Rows {
		user := trimmedCell(row, idx, "AUTHORIZATION_NAME")
		if user == "" {
			continue
		}
		asp := trimmedCell(row, idx, "ASPGRP")
		if asp == "" {
			asp = "*SYSBAS"
		}
		tags := []tags.Tag{
			hostTag,
			{Key: "user", Value: user},
			{Key: "asp", Value: asp},
		}

		used, usedOk := parseFloatCell(row, idx, "STORAGE_USED")
		if usedOk {
			totalUsedKB += used
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.user_storage.used_kb",
				Timestamp: ts,
				Value:     float32(used),
				Tags:      tags,
			})
		}

		// Quota parsing: the column is either a KB count or
		// "*NOMAX". We emit quota + ratio only when a numeric
		// value was set.
		rawQuota, present, _ := requireCell(row, idx, "MAXIMUM_STORAGE_ALLOWED")
		if !present {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(rawQuota), "*NOMAX") {
			continue
		}
		quota, quotaOk := parseFloatCell(row, idx, "MAXIMUM_STORAGE_ALLOWED")
		if !quotaOk || quota <= 0 {
			continue
		}
		usersWithQuota++
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.user_storage.quota_kb",
			Timestamp: ts,
			Value:     float32(quota),
			Tags:      tags,
		})
		if usedOk {
			ratio := (used / quota) * 100
			if ratio >= 80 {
				usersOver80Pct++
			}
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.user_storage.usage_ratio_percent",
				Timestamp: ts,
				Value:     float32(ratio),
				Tags:      tags,
			})
		}
	}

	points = append(points,
		datapoint.DataPoint{
			Name:      "ibmi.user_storage.users_total",
			Timestamp: ts,
			Value:     float32(len(res.Rows)),
			Tags:      []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name:      "ibmi.user_storage.used_total_kb",
			Timestamp: ts,
			Value:     float32(totalUsedKB),
			Tags:      []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name:      "ibmi.user_storage.users_with_quota",
			Timestamp: ts,
			Value:     float32(usersWithQuota),
			Tags:      []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name:      "ibmi.user_storage.users_over_80pct",
			Timestamp: ts,
			Value:     float32(usersOver80Pct),
			Tags:      []tags.Tag{hostTag},
		},
	)

	if len(points) == 0 {
		return nil, fmt.Errorf("USER_STORAGE produced no usable datapoints")
	}
	return points, nil
}
