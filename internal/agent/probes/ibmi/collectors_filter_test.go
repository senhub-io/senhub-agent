package ibmi

import "testing"

// TestFilterCollectors_DefaultReturnsEverything asserts that a probe
// with no enable/disable config gets the full default set.
func TestFilterCollectors_DefaultReturnsEverything(t *testing.T) {
	got := filterCollectors(probeConfig{})
	want := len(defaultCollectors())
	if len(got) != want {
		t.Errorf("default filter: want %d collectors, got %d", want, len(got))
	}
}

// TestFilterCollectors_EnabledAllowlist isolates a single collector
// from the full set via EnabledCollectors.
func TestFilterCollectors_EnabledAllowlist(t *testing.T) {
	got := filterCollectors(probeConfig{
		EnabledCollectors: []string{"system_status", "asp"},
	})
	if len(got) != 2 {
		t.Fatalf("allowlist: want 2, got %d", len(got))
	}
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name()] = true
	}
	if !names["system_status"] || !names["asp"] {
		t.Errorf("allowlist did not preserve the requested names: %v", names)
	}
}

// TestFilterCollectors_DisabledDenylistAppliedToDefaults excludes
// two collectors from the default set.
func TestFilterCollectors_DisabledDenylistAppliedToDefaults(t *testing.T) {
	got := filterCollectors(probeConfig{
		DisabledCollectors: []string{"sys_table_stats", "jvm"},
	})
	want := len(defaultCollectors()) - 2
	if len(got) != want {
		t.Errorf("denylist: want %d collectors, got %d", want, len(got))
	}
	for _, c := range got {
		if c.Name() == "sys_table_stats" || c.Name() == "jvm" {
			t.Errorf("denied collector %s was still returned", c.Name())
		}
	}
}

// TestFilterCollectors_CanOptInAuditJournal verifies that a collector
// which is NOT in defaultCollectors (audit_journal) can still be
// activated via EnabledCollectors.
func TestFilterCollectors_CanOptInAuditJournal(t *testing.T) {
	got := filterCollectors(probeConfig{
		EnabledCollectors: []string{"system_status", "audit_journal"},
	})
	if len(got) != 2 {
		t.Fatalf("opt-in: want 2, got %d", len(got))
	}
	foundAudit := false
	for _, c := range got {
		if c.Name() == "audit_journal" {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Error("audit_journal should have been activated via the allowlist")
	}
}

// TestFilterCollectors_UnknownNamesAreIgnored guards against typos
// or obsolete config entries silently breaking a deployment.
func TestFilterCollectors_UnknownNamesAreIgnored(t *testing.T) {
	got := filterCollectors(probeConfig{
		EnabledCollectors: []string{"system_status", "no_such_collector"},
	})
	if len(got) != 1 || got[0].Name() != "system_status" {
		t.Errorf("unknown names should be dropped, got %d collectors", len(got))
	}
}
