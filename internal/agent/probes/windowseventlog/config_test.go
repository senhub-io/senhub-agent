package windowseventlog

import (
	"testing"
	"time"
)

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"channels": []interface{}{"System"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PollInterval != DefaultPollInterval {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, DefaultPollInterval)
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0] != "System" {
		t.Errorf("Channels = %v", cfg.Channels)
	}
	if len(cfg.levelInts) != 0 {
		t.Errorf("levelInts should be empty, got %v", cfg.levelInts)
	}
	if cfg.Backlog || cfg.RedactPII {
		t.Errorf("Backlog/RedactPII should default false")
	}
}

func TestParseConfig_AllFields(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"channels":          []interface{}{"System", "Application", "Citrix-XenDesktop-VdaPlugin/Operational"},
		"levels":            []interface{}{"Critical", "Error", "Warning"},
		"include_event_ids": []interface{}{1001, 1024},
		"exclude_event_ids": []interface{}{4624},
		"sources":           []interface{}{"Citrix*", "FSLogix*"},
		"poll_interval":     "45s",
		"bookmark_path":     `C:\ProgramData\SenHub\bookmarks\vda.xml`,
		"backlog":           true,
		"redact_pii":        true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Channels) != 3 {
		t.Errorf("Channels = %v", cfg.Channels)
	}
	if want := []int{1, 2, 3}; !equalInts(cfg.levelInts, want) {
		t.Errorf("levelInts = %v, want %v", cfg.levelInts, want)
	}
	if want := []int{1001, 1024}; !equalInts(cfg.IncludeEventIDs, want) {
		t.Errorf("IncludeEventIDs = %v, want %v", cfg.IncludeEventIDs, want)
	}
	if want := []int{4624}; !equalInts(cfg.ExcludeEventIDs, want) {
		t.Errorf("ExcludeEventIDs = %v, want %v", cfg.ExcludeEventIDs, want)
	}
	if cfg.PollInterval != 45*time.Second {
		t.Errorf("PollInterval = %v", cfg.PollInterval)
	}
	if !cfg.Backlog || !cfg.RedactPII {
		t.Errorf("Backlog/RedactPII should be true")
	}
	if cfg.BookmarkPath == "" {
		t.Errorf("BookmarkPath not set")
	}
}

func TestParseConfig_RequiresChannel(t *testing.T) {
	if _, err := parseConfig(map[string]interface{}{}); err == nil {
		t.Fatal("expected error when no channels configured")
	}
	if _, err := parseConfig(map[string]interface{}{"channels": []interface{}{""}}); err == nil {
		t.Fatal("expected error when channels list is all-empty")
	}
}

func TestParseConfig_RejectsUnknownLevel(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"channels": []interface{}{"System"},
		"levels":   []interface{}{"Fatal"},
	})
	if err == nil {
		t.Fatal("expected error for unknown level name")
	}
}

func TestParseConfig_RejectsBadEventID(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"channels":          []interface{}{"System"},
		"include_event_ids": []interface{}{"not-a-number"},
	})
	if err == nil {
		t.Fatal("expected error for non-integer event id")
	}
}

func TestParseConfig_PollIntervalBareSeconds(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"channels":      []interface{}{"System"},
		"poll_interval": 30,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", cfg.PollInterval)
	}
}

func TestLevelTextToInt(t *testing.T) {
	cases := map[string]int{
		"Critical": 1, "error": 2, "WARNING": 3, "Information": 4, "info": 4, "Verbose": 5,
	}
	for text, want := range cases {
		got, ok := levelTextToInt(text)
		if !ok || got != want {
			t.Errorf("levelTextToInt(%q) = %d,%v want %d,true", text, got, ok, want)
		}
	}
	if _, ok := levelTextToInt("bogus"); ok {
		t.Error("levelTextToInt(bogus) should not be ok")
	}
}

func TestBuildXPathQuery(t *testing.T) {
	if got := buildXPathQuery(nil); got != "*" {
		t.Errorf("no levels: got %q want *", got)
	}
	if got := buildXPathQuery([]int{1, 2}); got != "*[System[(Level=1 or Level=2)]]" {
		t.Errorf("two levels: got %q", got)
	}
}

func TestDurationParam(t *testing.T) {
	if d, ok, err := durationParam("5m"); err != nil || !ok || d != 5*time.Minute {
		t.Errorf("string: %v %v %v", d, ok, err)
	}
	if _, ok, _ := durationParam(nil); ok {
		t.Error("nil should report ok=false")
	}
	if _, _, err := durationParam("nonsense"); err == nil {
		t.Error("invalid duration should error")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("short: %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc…" {
		t.Errorf("long: %q", got)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
