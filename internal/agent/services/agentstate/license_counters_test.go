package agentstate

import "testing"

func TestFreshStateHasNoLicenseInvalidSeries(t *testing.T) {
	ResetLicenseInvalidForTest()
	t.Cleanup(ResetLicenseInvalidForTest)

	got := GetLicenseInvalidByReason()
	if len(got) != 0 {
		t.Fatalf("expected empty map when no licence is configured, got %v", got)
	}
}

func TestClearLicenseInvalidPublishesAllKnownReasonsAtZero(t *testing.T) {
	ResetLicenseInvalidForTest()
	t.Cleanup(ResetLicenseInvalidForTest)

	ClearLicenseInvalid()

	got := GetLicenseInvalidByReason()
	want := map[string]uint64{
		"validation_failed": 0,
		"binding_mismatch":  0,
		"expired_no_grace":  0,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d reasons, got %d: %v", len(want), len(got), got)
	}
	for reason, wantVal := range want {
		if got[reason] != wantVal {
			t.Errorf("reason %q: expected %d, got %d", reason, wantVal, got[reason])
		}
	}
}

func TestClearAfterSetResetsAllKnownReasons(t *testing.T) {
	ResetLicenseInvalidForTest()
	t.Cleanup(ResetLicenseInvalidForTest)

	SetLicenseInvalid("binding_mismatch")
	ClearLicenseInvalid()

	got := GetLicenseInvalidByReason()
	if len(got) != 3 {
		t.Fatalf("expected 3 reasons after clear, got %d: %v", len(got), got)
	}
	for _, reason := range []string{"validation_failed", "binding_mismatch", "expired_no_grace"} {
		if got[reason] != 0 {
			t.Errorf("reason %q: expected 0, got %d", reason, got[reason])
		}
	}
}

func TestSetAfterClearKeepsOtherReasonsAtZero(t *testing.T) {
	ResetLicenseInvalidForTest()
	t.Cleanup(ResetLicenseInvalidForTest)

	ClearLicenseInvalid()
	SetLicenseInvalid("expired_no_grace")

	got := GetLicenseInvalidByReason()
	if got["expired_no_grace"] != 1 {
		t.Errorf("expired_no_grace: expected 1, got %d", got["expired_no_grace"])
	}
	if got["validation_failed"] != 0 {
		t.Errorf("validation_failed: expected 0, got %d", got["validation_failed"])
	}
	if got["binding_mismatch"] != 0 {
		t.Errorf("binding_mismatch: expected 0, got %d", got["binding_mismatch"])
	}
}

func TestClearResetsUnknownReasonButKeepsKey(t *testing.T) {
	ResetLicenseInvalidForTest()
	t.Cleanup(ResetLicenseInvalidForTest)

	SetLicenseInvalid("other")
	ClearLicenseInvalid()

	got := GetLicenseInvalidByReason()
	if len(got) != 4 {
		t.Fatalf("expected 4 reasons (3 known + other), got %d: %v", len(got), got)
	}
	if got["other"] != 0 {
		t.Errorf("other: expected 0 after clear, got %d", got["other"])
	}
}
