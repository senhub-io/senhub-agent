package filetail

import (
	"strings"
	"testing"
)

func TestParseConfig_RequiresPaths(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "paths") {
		t.Fatalf("expected paths error, got %v", err)
	}
}

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"paths": []interface{}{"/var/log/app.log"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Parser.Type != ParserRaw {
		t.Errorf("default parser=%q, want raw", cfg.Parser.Type)
	}
	if cfg.MaxBytesPerLine != DefaultMaxBytesPerLine {
		t.Errorf("MaxBytesPerLine=%d, want %d", cfg.MaxBytesPerLine, DefaultMaxBytesPerLine)
	}
	if cfg.FromBeginning {
		t.Errorf("FromBeginning should default false")
	}
	if len(cfg.Paths) != 1 || cfg.Paths[0] != "/var/log/app.log" {
		t.Errorf("Paths=%v", cfg.Paths)
	}
}

func TestParseConfig_SingleStringPath(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{"paths": "/tmp/x.log"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Paths) != 1 || cfg.Paths[0] != "/tmp/x.log" {
		t.Errorf("Paths=%v", cfg.Paths)
	}
}

func TestParseConfig_RegexRequiresPattern(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"paths":  []interface{}{"/x"},
		"parser": map[string]interface{}{"type": "regex"},
	})
	if err == nil || !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("expected pattern error, got %v", err)
	}
}

func TestParseConfig_RegexRequiresNamedGroups(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"paths":  []interface{}{"/x"},
		"parser": map[string]interface{}{"type": "regex", "pattern": "^(.+)$"},
	})
	if err == nil || !strings.Contains(err.Error(), "named capture") {
		t.Fatalf("expected named-capture error, got %v", err)
	}
}

func TestParseConfig_RegexBadPattern(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"paths":  []interface{}{"/x"},
		"parser": map[string]interface{}{"type": "regex", "pattern": "(?P<a>("},
	})
	if err == nil || !strings.Contains(err.Error(), "compiling parser.pattern") {
		t.Fatalf("expected compile error, got %v", err)
	}
}

func TestParseConfig_UnknownParserType(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"paths":  []interface{}{"/x"},
		"parser": map[string]interface{}{"type": "yaml"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown parser.type") {
		t.Fatalf("expected unknown-type error, got %v", err)
	}
}

func TestParseConfig_MultilineBadMatch(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"paths":     []interface{}{"/x"},
		"multiline": map[string]interface{}{"pattern": "^x", "match": "sideways"},
	})
	if err == nil || !strings.Contains(err.Error(), "multiline.match") {
		t.Fatalf("expected multiline.match error, got %v", err)
	}
}

func TestParseConfig_MultilineDefaultsToAfter(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"paths":     []interface{}{"/x"},
		"multiline": map[string]interface{}{"pattern": `^\d{4}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Multiline.Match != "after" {
		t.Errorf("multiline match=%q, want after", cfg.Multiline.Match)
	}
	if cfg.Multiline.compiled == nil {
		t.Errorf("multiline pattern not compiled")
	}
}

func TestParseConfig_MaxBytesRejectsNonPositive(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"paths":              []interface{}{"/x"},
		"max_bytes_per_line": 0,
	})
	if err == nil || !strings.Contains(err.Error(), "max_bytes_per_line") {
		t.Fatalf("expected max_bytes error, got %v", err)
	}
}

func TestParseConfig_FullCitrixExample(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"paths": []interface{}{
			`C:\Windows\Logs\Citrix\*.log`,
			`C:\ProgramData\Citrix\GroupPolicy\Logs\*.log`,
		},
		"multiline": map[string]interface{}{
			"pattern": `^\d{4}-\d{2}-\d{2}`,
			"negate":  false,
			"match":   "after",
		},
		"parser": map[string]interface{}{
			"type":             "regex",
			"pattern":          `^(?P<timestamp>\S+ \S+) \[(?P<level>\w+)\] (?P<component>\w+): (?P<message>.+)$`,
			"timestamp_field":  "timestamp",
			"timestamp_format": "2006-01-02 15:04:05.000",
		},
		"bookmark_path": "/var/lib/senhub/bookmarks/citrix_vda.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Paths) != 2 {
		t.Errorf("Paths=%v", cfg.Paths)
	}
	if cfg.Parser.Type != ParserRegex || cfg.Parser.compiled == nil {
		t.Errorf("regex parser not set up: %+v", cfg.Parser)
	}
	if cfg.Parser.TimestampField != "timestamp" {
		t.Errorf("timestamp_field=%q", cfg.Parser.TimestampField)
	}
	if cfg.BookmarkPath == "" {
		t.Errorf("bookmark_path lost")
	}
}
