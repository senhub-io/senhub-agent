package ibmi

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
)

func TestUserProfileCollector_AggregatesAndWatchlist(t *testing.T) {
	c := userProfileCollector{}
	res := &bridge.Result{
		Columns: []string{
			"AUTHORIZATION_NAME", "STATUS", "USER_CLASS_NAME",
			"SPECIAL_AUTHORITIES", "NO_PASSWORD_INDICATOR",
			"DAYS_UNTIL_PASSWORD_EXPIRES", "PREVIOUS_SIGNON",
			"SIGN_ON_ATTEMPTS_NOT_VALID", "GROUP_PROFILE_NAME",
		},
		Rows: [][]*string{
			{strPtr("QSECOFR"), strPtr("*ENABLED"), strPtr("*SECOFR"),
				strPtr("*ALLOBJ    *SECADM    *SAVSYS"),
				strPtr("NO"), strPtr("0"), strPtr("2026-04-15 08:00:00"),
				strPtr("0"), strPtr("*NONE")},
			{strPtr("MATTHIEU"), strPtr("*ENABLED"), strPtr("*PGMR"),
				nil, strPtr("NO"), strPtr("15"),
				strPtr("2026-04-14 10:00:00"), strPtr("0"), strPtr("*NONE")},
			{strPtr("ENGMTZ"), strPtr("*DISABLED"), strPtr("*USER"),
				nil, strPtr("YES"), strPtr("0"), nil, strPtr("3"), strPtr("GRP1")},
		},
	}
	points, err := c.Parse(res, "h", time.Now())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	byName := make(map[string][]float32, 8)
	for _, dp := range points {
		byName[dp.Name] = append(byName[dp.Name], dp.Value)
	}

	if byName["ibmi.user_profile.total"][0] != 3 {
		t.Errorf("total: want 3, got %v", byName["ibmi.user_profile.total"])
	}
	if byName["ibmi.user_profile.no_password_total"][0] != 1 {
		t.Errorf("no_password: want 1 (ENGMTZ), got %v", byName["ibmi.user_profile.no_password_total"])
	}
	if byName["ibmi.user_profile.password_expiring_30d"][0] != 1 {
		t.Errorf("expiring_30d: want 1 (MATTHIEU), got %v", byName["ibmi.user_profile.password_expiring_30d"])
	}
	if byName["ibmi.user_profile.failed_signon_total"][0] != 1 {
		t.Errorf("failed_signon: want 1 (ENGMTZ), got %v", byName["ibmi.user_profile.failed_signon_total"])
	}

	// Watchlist should emit exactly one privileged_status datapoint for QSECOFR.
	privileged := byName["ibmi.user_profile.privileged_status"]
	if len(privileged) != 1 {
		t.Errorf("privileged_status: expected 1, got %d", len(privileged))
	}
	if privileged[0] != 1 { // *ENABLED → 1
		t.Errorf("QSECOFR should be enabled (1), got %v", privileged[0])
	}
}

func TestSystemValueCollector_NumericAndStringValues(t *testing.T) {
	c := systemValueCollector{}
	res := &bridge.Result{
		Columns: []string{"SYSTEM_VALUE_NAME", "CURRENT_NUMERIC_VALUE", "CURRENT_CHARACTER_VALUE"},
		Rows: [][]*string{
			// Native numeric
			{strPtr("QSECURITY"), strPtr("40"), nil},
			// Numeric-encoded-as-string (IBM i quirk)
			{strPtr("QMAXSIGN"), nil, strPtr("000005")},
			// Genuine string enum
			{strPtr("QAUTOCFG"), nil, strPtr("*NO")},
			// Another string enum
			{strPtr("QDSPSGNINF"), nil, strPtr("*YES")},
		},
	}
	points, err := c.Parse(res, "h", time.Now())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(points) != 4 {
		t.Fatalf("expected 4 datapoints, got %d", len(points))
	}

	byMetric := make(map[string]float32, 4)
	for _, dp := range points {
		byMetric[dp.Name] = dp.Value
	}

	if byMetric["ibmi.sysval.security_level"] != 40 {
		t.Errorf("QSECURITY: want 40, got %v", byMetric["ibmi.sysval.security_level"])
	}
	if byMetric["ibmi.sysval.max_signon_attempts"] != 5 {
		t.Errorf("QMAXSIGN: want 5 (parsed from '000005'), got %v", byMetric["ibmi.sysval.max_signon_attempts"])
	}
	// *NO → 0, *YES → 1 (see hashStringToEnum).
	if byMetric["ibmi.sysval.auto_config"] != 0 {
		t.Errorf("QAUTOCFG (*NO): want 0, got %v", byMetric["ibmi.sysval.auto_config"])
	}
	if byMetric["ibmi.sysval.display_signon_info"] != 1 {
		t.Errorf("QDSPSGNINF (*YES): want 1, got %v", byMetric["ibmi.sysval.display_signon_info"])
	}
}

func TestSystemValueCollector_EmptyResultIsError(t *testing.T) {
	c := systemValueCollector{}
	_, err := c.Parse(&bridge.Result{Columns: []string{"SYSTEM_VALUE_NAME"}, Rows: nil}, "h", time.Now())
	if err == nil {
		t.Fatal("expected error when watchlist query returns no rows")
	}
}
