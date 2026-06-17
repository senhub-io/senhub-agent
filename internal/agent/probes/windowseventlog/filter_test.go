package windowseventlog

import "testing"

func ev(id, level int, provider string) parsedEvent {
	return parsedEvent{EventID: id, Level: level, Provider: provider}
}

func TestShouldEmit_NoFiltersMatchesAll(t *testing.T) {
	var c WindowsEventLogProbeConfig
	if !c.shouldEmit(ev(1000, 4, "Anything")) {
		t.Error("empty config should match every event")
	}
}

func TestShouldEmit_LevelFilter(t *testing.T) {
	c := WindowsEventLogProbeConfig{levelInts: []int{1, 2}}
	if !c.shouldEmit(ev(1, 2, "P")) {
		t.Error("Error level should pass")
	}
	if c.shouldEmit(ev(1, 4, "P")) {
		t.Error("Information level should be filtered out")
	}
}

func TestShouldEmit_IncludeExcludeEventIDs(t *testing.T) {
	c := WindowsEventLogProbeConfig{IncludeEventIDs: []int{1001, 1024}, ExcludeEventIDs: []int{1024}}
	if !c.shouldEmit(ev(1001, 2, "P")) {
		t.Error("1001 is included")
	}
	if c.shouldEmit(ev(1024, 2, "P")) {
		t.Error("1024 excluded takes precedence over include")
	}
	if c.shouldEmit(ev(9999, 2, "P")) {
		t.Error("event not in include-list should be dropped")
	}
}

func TestShouldEmit_SourceGlob(t *testing.T) {
	c := WindowsEventLogProbeConfig{Sources: []string{"Citrix*", "FSLogix*"}}
	if !c.shouldEmit(ev(1, 2, "Citrix-XenDesktop-VdaPlugin")) {
		t.Error("Citrix* should match")
	}
	if !c.shouldEmit(ev(1, 2, "fslogix-apps")) {
		t.Error("glob match should be case-insensitive")
	}
	if c.shouldEmit(ev(1, 2, "Microsoft-Windows-Kernel")) {
		t.Error("non-matching provider should be dropped")
	}
}

func TestIsSensitiveField(t *testing.T) {
	if !isSensitiveField("TargetUserName") || !isSensitiveField("ipaddress") {
		t.Error("known PII fields should be flagged (case-insensitive)")
	}
	if isSensitiveField("LogonType") {
		t.Error("LogonType is not PII")
	}
}

func TestRedactSecurityBody(t *testing.T) {
	if got := redactSecurityBody("Security", "user jdoe logged on"); got == "user jdoe logged on" {
		t.Error("Security body should be replaced")
	}
	if got := redactSecurityBody("Application", "app started"); got != "app started" {
		t.Errorf("non-Security body must be untouched, got %q", got)
	}
}
