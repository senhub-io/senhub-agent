package ibmi

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// userProfileCollector reads QSYS2.USER_INFO and emits both aggregate
// counts (by STATUS, by *SPECIAL authorities) and a bounded-cardinality
// watchlist of privileged profiles (those with *ALLOBJ, *SECADM,
// *SECOFR or *AUDIT).
//
// This is the first security-oriented collector and its design trade-off
// is the classic "visibility vs cardinality" one. A large IBM i system
// can have thousands of user profiles; emitting one DataPoint per user
// would blow up the time series count. Instead we:
//
//  1. aggregate every user into a small number of count buckets
//     (STATUS, NO_PASSWORD, expired-in-next-30d, by special authority)
//  2. only emit per-user details for the **privileged** users whose
//     SPECIAL_AUTHORITIES field contains *ALLOBJ / *SECADM / *SECOFR /
//     *AUDIT — auditors care about those individually
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-user-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type userProfileCollector struct{}

func (userProfileCollector) Name() string  { return "user_profile" }
func (userProfileCollector) IsEvent() bool { return false }

// privilegedAuthorities is the fixed list of IBM i *SPECIAL authorities
// that warrant per-user emission. The SQL layer cannot filter on this
// column reliably (SPECIAL_AUTHORITIES is a space-separated string), so
// the filtering happens in Parse.
var privilegedAuthorities = []string{"*ALLOBJ", "*SECADM", "*SECOFR", "*AUDIT"}

func (userProfileCollector) SQL() string {
	return "SELECT AUTHORIZATION_NAME, STATUS, USER_CLASS_NAME, SPECIAL_AUTHORITIES, NO_PASSWORD_INDICATOR, DAYS_UNTIL_PASSWORD_EXPIRES, PREVIOUS_SIGNON, SIGN_ON_ATTEMPTS_NOT_VALID, GROUP_PROFILE_NAME FROM QSYS2.USER_INFO"
}

func (userProfileCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		return nil, fmt.Errorf("USER_INFO returned no rows")
	}
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	statusCounts := make(map[string]int, 4)
	userClassCounts := make(map[string]int, 6)
	specialAuthCounts := make(map[string]int, 8)
	var (
		totalProfiles    int
		noPassword       int
		expiringIn30Days int
		failedSignons    int
	)
	var privilegedPoints []datapoint.DataPoint

	for _, row := range res.Rows {
		name := trimmedCell(row, idx, "AUTHORIZATION_NAME")
		if name == "" {
			continue
		}
		totalProfiles++

		status := trimmedCell(row, idx, "STATUS")
		statusCounts[status]++

		userClass := trimmedCell(row, idx, "USER_CLASS_NAME")
		userClassCounts[userClass]++

		if trimmedCell(row, idx, "NO_PASSWORD_INDICATOR") == "YES" {
			noPassword++
		}

		if days, ok := parseFloatCell(row, idx, "DAYS_UNTIL_PASSWORD_EXPIRES"); ok && days > 0 && days <= 30 {
			expiringIn30Days++
		}

		if fails, ok := parseFloatCell(row, idx, "SIGN_ON_ATTEMPTS_NOT_VALID"); ok && fails > 0 {
			failedSignons++
		}

		specialAuths := trimmedCell(row, idx, "SPECIAL_AUTHORITIES")
		if specialAuths == "" {
			specialAuthCounts["<none>"]++
			continue
		}

		// SPECIAL_AUTHORITIES is a space-separated string like
		// "*ALLOBJ    *SAVSYS    *JOBCTL". Normalise and index every
		// token so we can both count them globally and decide whether
		// this user qualifies for the watchlist.
		tokens := strings.Fields(specialAuths)
		isPrivileged := false
		for _, tok := range tokens {
			specialAuthCounts[tok]++
			for _, priv := range privilegedAuthorities {
				if tok == priv {
					isPrivileged = true
				}
			}
		}

		if isPrivileged {
			privilegedPoints = append(privilegedPoints, datapoint.DataPoint{
				Name:      "ibmi.user_profile.privileged_status",
				Timestamp: ts,
				Value:     statusToNumeric(status),
				Tags: []tags.Tag{
					hostTag,
					{Key: "user_name", Value: name},
					{Key: "status", Value: status},
					{Key: "user_class", Value: userClass},
					{Key: "special_auths", Value: specialAuths},
					{Key: "group_profile", Value: trimmedCell(row, idx, "GROUP_PROFILE_NAME")},
				},
			})
		}
	}

	points := make([]datapoint.DataPoint, 0, len(statusCounts)+len(specialAuthCounts)+len(userClassCounts)+len(privilegedPoints)+5)

	// Global counts
	points = append(points,
		datapoint.DataPoint{
			Name: "ibmi.user_profile.total", Timestamp: ts,
			Value: float32(totalProfiles), Tags: []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name: "ibmi.user_profile.no_password_total", Timestamp: ts,
			Value: float32(noPassword), Tags: []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name: "ibmi.user_profile.password_expiring_30d", Timestamp: ts,
			Value: float32(expiringIn30Days), Tags: []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name: "ibmi.user_profile.failed_signon_total", Timestamp: ts,
			Value: float32(failedSignons), Tags: []tags.Tag{hostTag},
		},
	)

	for status, count := range statusCounts {
		points = append(points, datapoint.DataPoint{
			Name: "ibmi.user_profile.count_by_status", Timestamp: ts,
			Value: float32(count),
			Tags:  []tags.Tag{hostTag, {Key: "status", Value: status}},
		})
	}
	for userClass, count := range userClassCounts {
		points = append(points, datapoint.DataPoint{
			Name: "ibmi.user_profile.count_by_class", Timestamp: ts,
			Value: float32(count),
			Tags:  []tags.Tag{hostTag, {Key: "user_class", Value: userClass}},
		})
	}
	for auth, count := range specialAuthCounts {
		points = append(points, datapoint.DataPoint{
			Name: "ibmi.user_profile.count_by_special_auth", Timestamp: ts,
			Value: float32(count),
			Tags:  []tags.Tag{hostTag, {Key: "special_auth", Value: auth}},
		})
	}

	points = append(points, privilegedPoints...)
	return points, nil
}

// statusToNumeric maps the IBM i *ENABLED/*DISABLED string to a gauge
// value: 1 = enabled, 0 = disabled, -1 = unknown. Useful for
// dashboards that want to plot a privileged user's availability
// over time.
func statusToNumeric(status string) float32 {
	switch strings.TrimSpace(status) {
	case "*ENABLED":
		return 1
	case "*DISABLED":
		return 0
	default:
		return -1
	}
}
