package ibmi

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// systemValueCollector snapshots a curated set of security-relevant
// IBM i system values from QSYS2.SYSTEM_VALUE_INFO. Each system value
// becomes a per-value DataPoint tagged with its name, so dashboards
// can pin a single tile to each control.
//
// Why a curated list: SYSTEM_VALUE_INFO exposes ~150 system values
// and the majority are operational settings (QDATFMT, QKBDTYPE, …)
// that aren't interesting from a security or observability standpoint.
// The watchlist is frozen in code on purpose — a security auditor
// wants to know every change to QSECURITY, not a flood of irrelevant
// locale knobs.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-system-value-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15):
//
//	SYSTEM_VALUE_NAME, CURRENT_NUMERIC_VALUE, CURRENT_CHARACTER_VALUE
//
// System values are either numeric OR string. A numeric one has
// CURRENT_NUMERIC_VALUE set and CURRENT_CHARACTER_VALUE null (and
// vice-versa). Some of the string ones encode numeric values as
// left-padded strings (QMAXSIGN = "000005"), so we try the numeric
// path first then fall back to parsing the character value.
type systemValueCollector struct{}

func (systemValueCollector) Name() string  { return "system_value" }
func (systemValueCollector) IsEvent() bool { return false }

// securityWatchlist is the curated set of system values that matter
// for an IBM i security posture review. Every entry maps to a fixed
// metric name so dashboards can alert on specific controls.
var securityWatchlist = []struct {
	sysval string
	metric string
}{
	{"QSECURITY", "ibmi.sysval.security_level"},
	{"QMAXSIGN", "ibmi.sysval.max_signon_attempts"},
	{"QPWDEXPITV", "ibmi.sysval.password_expiration_days"},
	{"QPWDMINLEN", "ibmi.sysval.password_min_length"},
	{"QPWDMAXLEN", "ibmi.sysval.password_max_length"},
	{"QPWDLVL", "ibmi.sysval.password_level"},
	{"QPWDRQDDIF", "ibmi.sysval.password_required_difference"},
	{"QLMTSECOFR", "ibmi.sysval.limit_security_officer"},
	{"QLMTDEVSSN", "ibmi.sysval.limit_device_sessions"},
	{"QAUTOCFG", "ibmi.sysval.auto_config"},
	{"QAUTOVRT", "ibmi.sysval.auto_virtual_devices"},
	{"QCRTAUT", "ibmi.sysval.create_default_auth"},
	{"QDSPSGNINF", "ibmi.sysval.display_signon_info"},
	{"QRMTSIGN", "ibmi.sysval.remote_signon"},
	{"QSHRMEMCTL", "ibmi.sysval.shared_memory_control"},
	{"QRETSVRSEC", "ibmi.sysval.retain_server_security"},
	{"QAUDCTL", "ibmi.sysval.audit_control"},
	{"QAUDLVL", "ibmi.sysval.audit_level"},
}

func (systemValueCollector) SQL() string {
	names := make([]string, 0, len(securityWatchlist))
	for _, e := range securityWatchlist {
		names = append(names, "'"+e.sysval+"'")
	}
	return fmt.Sprintf(
		"SELECT SYSTEM_VALUE_NAME, CURRENT_NUMERIC_VALUE, CURRENT_CHARACTER_VALUE FROM QSYS2.SYSTEM_VALUE_INFO WHERE SYSTEM_VALUE_NAME IN (%s)",
		strings.Join(names, ","),
	)
}

func (systemValueCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("SYSTEM_VALUE_INFO returned no rows for the security watchlist")
	}
	idx := columnIndex(res.Columns)

	// Build a name -> metric map for O(1) lookup during row iteration.
	nameToMetric := make(map[string]string, len(securityWatchlist))
	for _, e := range securityWatchlist {
		nameToMetric[e.sysval] = e.metric
	}

	hostTag := tags.Tag{Key: "host", Value: host}
	points := make([]datapoint.DataPoint, 0, len(res.Rows))

	for _, row := range res.Rows {
		name := trimmedCell(row, idx, "SYSTEM_VALUE_NAME")
		metric, ok := nameToMetric[name]
		if !ok {
			continue
		}

		var value float32
		var rawStr string

		if num, ok := parseFloatCell(row, idx, "CURRENT_NUMERIC_VALUE"); ok {
			value = float32(num)
			rawStr = strconv.FormatFloat(num, 'f', -1, 64)
		} else {
			raw := trimmedCell(row, idx, "CURRENT_CHARACTER_VALUE")
			rawStr = raw
			if raw == "" {
				continue
			}
			if parsed, err := strconv.ParseFloat(strings.TrimLeft(raw, "0 "), 64); err == nil && raw != "" && !strings.ContainsAny(raw, "*ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") {
				// Numeric-encoded-as-string case (e.g. "000005").
				value = float32(parsed)
			} else {
				// Genuine string value (e.g. "*NO", "*YES"). Hash
				// to a stable enum-like float so dashboards can
				// still plot it, but keep the raw value in a tag
				// for humans to read.
				value = float32(hashStringToEnum(raw))
			}
		}

		points = append(points, datapoint.DataPoint{
			Name:      metric,
			Timestamp: ts,
			Value:     value,
			Tags: []tags.Tag{
				hostTag,
				{Key: "sysval", Value: name},
				{Key: "raw_value", Value: rawStr},
			},
		})
	}

	return points, nil
}

// hashStringToEnum converts a string system value to a small stable
// integer encoding. Common IBM i string enum values get fixed codes,
// everything else hashes into a 32-bit range. The point is to give
// Prometheus/Grafana a numeric to plot while keeping the exact string
// in the raw_value tag.
func hashStringToEnum(s string) int {
	switch strings.TrimSpace(s) {
	case "*NO", "*NONE":
		return 0
	case "*YES", "*ALL":
		return 1
	case "*SYSVAL":
		return 2
	case "*FRCRLS":
		return 3
	case "*VERIFY":
		return 4
	}
	// Fallback: FNV-1a-ish stable hash in [100, 999] so it doesn't
	// collide with the meaningful small values above.
	h := 2166136261
	for _, c := range s {
		h ^= int(c)
		h *= 16777619
	}
	return 100 + (h&0x7fffffff)%900
}
