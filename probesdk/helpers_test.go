package probesdk_test

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/probesdk/configparser"
	"senhub-agent.go/probesdk/dbcommon"
	"senhub-agent.go/probesdk/entity"
	"senhub-agent.go/probesdk/license"
	"senhub-agent.go/probesdk/logparse"
	"senhub-agent.go/probesdk/sanitize"
	"senhub-agent.go/probesdk/state"
	"senhub-agent.go/probesdk/tags"
	"senhub-agent.go/probesdk/types"
	"senhub-agent.go/probesdk/validators"
)

func TestTypesParamHelpers(t *testing.T) {
	t.Run("IntParam", func(t *testing.T) {
		tests := []struct {
			name   string
			params map[string]interface{}
			key    string
			want   int
			wantOK bool
		}{
			{"int", map[string]interface{}{"k": 42}, "k", 42, true},
			{"integral float64", map[string]interface{}{"k": float64(42)}, "k", 42, true},
			{"numeric string", map[string]interface{}{"k": "42"}, "k", 42, true},
			{"fractional float", map[string]interface{}{"k": float64(42.5)}, "k", 0, false},
			{"wrong type", map[string]interface{}{"k": []string{"a"}}, "k", 0, false},
			{"absent", map[string]interface{}{}, "k", 0, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, gotOK := types.IntParam(tt.params, tt.key)
				if got != tt.want || gotOK != tt.wantOK {
					t.Fatalf("IntParam() = (%d, %v), want (%d, %v)", got, gotOK, tt.want, tt.wantOK)
				}
			})
		}
	})

	t.Run("FloatParam", func(t *testing.T) {
		tests := []struct {
			name   string
			params map[string]interface{}
			key    string
			want   float64
			wantOK bool
		}{
			{"int", map[string]interface{}{"k": 42}, "k", 42, true},
			{"float64", map[string]interface{}{"k": float64(1.5)}, "k", 1.5, true},
			{"numeric string", map[string]interface{}{"k": "1.5"}, "k", 1.5, true},
			{"wrong type", map[string]interface{}{"k": true}, "k", 0, false},
			{"absent", map[string]interface{}{}, "k", 0, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, gotOK := types.FloatParam(tt.params, tt.key)
				if got != tt.want || gotOK != tt.wantOK {
					t.Fatalf("FloatParam() = (%v, %v), want (%v, %v)", got, gotOK, tt.want, tt.wantOK)
				}
			})
		}
	})

	t.Run("StringParam", func(t *testing.T) {
		tests := []struct {
			name   string
			params map[string]interface{}
			key    string
			want   string
			wantOK bool
		}{
			{"string", map[string]interface{}{"k": "x"}, "k", "x", true},
			{"wrong type", map[string]interface{}{"k": 42}, "k", "", false},
			{"absent", map[string]interface{}{}, "k", "", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, gotOK := types.StringParam(tt.params, tt.key)
				if got != tt.want || gotOK != tt.wantOK {
					t.Fatalf("StringParam() = (%q, %v), want (%q, %v)", got, gotOK, tt.want, tt.wantOK)
				}
			})
		}
	})

	t.Run("BoolParam", func(t *testing.T) {
		tests := []struct {
			name   string
			params map[string]interface{}
			key    string
			want   bool
			wantOK bool
		}{
			{"bool", map[string]interface{}{"k": true}, "k", true, true},
			{"string true", map[string]interface{}{"k": "true"}, "k", true, true},
			{"string false", map[string]interface{}{"k": "false"}, "k", false, true},
			{"string unparsable", map[string]interface{}{"k": "no"}, "k", false, false},
			{"wrong type", map[string]interface{}{"k": 1}, "k", false, false},
			{"absent", map[string]interface{}{}, "k", false, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, gotOK := types.BoolParam(tt.params, tt.key)
				if got != tt.want || gotOK != tt.wantOK {
					t.Fatalf("BoolParam() = (%v, %v), want (%v, %v)", got, gotOK, tt.want, tt.wantOK)
				}
			})
		}
	})

	t.Run("StringSlice", func(t *testing.T) {
		tests := []struct {
			name string
			raw  interface{}
			want []string
		}{
			{"list interface", []interface{}{"a", "b", "", 3}, []string{"a", "b"}},
			{"list strings", []string{"a", "", "b"}, []string{"a", "b"}},
			{"scalar", "a", []string{"a"}},
			{"wrong type", 42, nil},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := types.StringSlice(tt.raw)
				if len(got) != len(tt.want) {
					t.Fatalf("StringSlice() len = %d, want %d", len(got), len(tt.want))
				}
				for i := range tt.want {
					if got[i] != tt.want[i] {
						t.Fatalf("StringSlice()[%d] = %q, want %q", i, got[i], tt.want[i])
					}
				}
			})
		}
	})

	t.Run("StringSliceParam", func(t *testing.T) {
		tests := []struct {
			name   string
			params map[string]interface{}
			key    string
			want   []string
			wantOK bool
		}{
			{"list strings", map[string]interface{}{"k": []string{"a", "b"}}, "k", []string{"a", "b"}, true},
			{"scalar", map[string]interface{}{"k": "a"}, "k", []string{"a"}, true},
			{"empty list", map[string]interface{}{"k": []interface{}{}}, "k", []string{}, true},
			{"wrong type", map[string]interface{}{"k": 42}, "k", nil, false},
			{"absent", map[string]interface{}{}, "k", nil, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, gotOK := types.StringSliceParam(tt.params, tt.key)
				if gotOK != tt.wantOK {
					t.Fatalf("StringSliceParam() ok = %v, want %v", gotOK, tt.wantOK)
				}
				if len(got) != len(tt.want) {
					t.Fatalf("StringSliceParam() len = %d, want %d", len(got), len(tt.want))
				}
				for i := range tt.want {
					if got[i] != tt.want[i] {
						t.Fatalf("StringSliceParam()[%d] = %q, want %q", i, got[i], tt.want[i])
					}
				}
			})
		}
	})

	t.Run("DurationParam", func(t *testing.T) {
		tests := []struct {
			name   string
			params map[string]interface{}
			key    string
			want   time.Duration
			wantOK bool
		}{
			{"int seconds", map[string]interface{}{"k": 2}, "k", 2 * time.Second, true},
			{"duration string", map[string]interface{}{"k": "30ms"}, "k", 30 * time.Millisecond, true},
			{"numeric string seconds", map[string]interface{}{"k": "45"}, "k", 45 * time.Second, true},
			{"wrong type", map[string]interface{}{"k": true}, "k", 0, false},
			{"absent", map[string]interface{}{}, "k", 0, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, gotOK := types.DurationParam(tt.params, tt.key)
				if got != tt.want || gotOK != tt.wantOK {
					t.Fatalf("DurationParam() = (%v, %v), want (%v, %v)", got, gotOK, tt.want, tt.wantOK)
				}
			})
		}
	})
}

func TestSanitizeHelpers(t *testing.T) {
	now := time.Now()
	past := now.Add(-2 * time.Second)
	future := now.Add(time.Hour)
	zero := time.Time{}

	if _, ok := sanitize.Duration(nil, now); ok {
		t.Fatal("sanitize.Duration(nil) returned ok=true")
	}
	if _, ok := sanitize.Duration(&zero, now); ok {
		t.Fatal("sanitize.Duration(zero) returned ok=true")
	}
	if got, ok := sanitize.Duration(&future, now); !ok || got != 0 {
		t.Fatalf("sanitize.Duration(future) = (%v, %v), want (0, true)", got, ok)
	}
	gotDur, ok := sanitize.Duration(&past, now)
	if !ok {
		t.Fatal("sanitize.Duration(past) returned ok=false")
	}
	if diff := math.Abs(gotDur - now.Sub(past).Seconds()); diff > 0.001 {
		t.Fatalf("sanitize.Duration(past) = %v, want about %v", gotDur, now.Sub(past).Seconds())
	}

	if v, ok := sanitize.CountInt32(-1); ok || v != 0 {
		t.Fatalf("sanitize.CountInt32(-1) = (%v, %v), want (0, false)", v, ok)
	}
	if v, ok := sanitize.CountInt32(10); !ok || v != 10 {
		t.Fatalf("sanitize.CountInt32(10) = (%v, %v), want (10, true)", v, ok)
	}
	if v, ok := sanitize.CountInt32(int64(math.MaxInt32) + 1); ok || v != sanitize.MaxInt32 {
		t.Fatalf("sanitize.CountInt32(MaxInt32+1) = (%v, %v), want (%v, false)", v, ok, sanitize.MaxInt32)
	}

	if v, ok := sanitize.Bytes(-1); ok || v != 0 {
		t.Fatalf("sanitize.Bytes(-1) = (%v, %v), want (0, false)", v, ok)
	}
	if v, ok := sanitize.Bytes(2048); !ok || v != 2048 {
		t.Fatalf("sanitize.Bytes(2048) = (%v, %v), want (2048, true)", v, ok)
	}

	mapping := map[string]float64{"one": 1, "Two": 2}
	if v, ok := sanitize.EnumValue("one", mapping); !ok || v != 1 {
		t.Fatalf("sanitize.EnumValue(one) = (%v, %v), want (1, true)", v, ok)
	}
	if v, ok := sanitize.EnumValue("two", mapping); !ok || v != 2 {
		t.Fatalf("sanitize.EnumValue(two) = (%v, %v), want (2, true)", v, ok)
	}
	if _, ok := sanitize.EnumValue("three", mapping); ok {
		t.Fatal("sanitize.EnumValue(three) returned ok=true")
	}

	if !sanitize.IsFinite(1.0) {
		t.Fatal("sanitize.IsFinite(1.0) = false, want true")
	}
	if sanitize.IsFinite(math.NaN()) {
		t.Fatal("sanitize.IsFinite(NaN) = true, want false")
	}
	if sanitize.IsFinite(math.Inf(1)) {
		t.Fatal("sanitize.IsFinite(Inf) = true, want false")
	}
}

func TestValidators(t *testing.T) {
	tests := []struct {
		name string
		fn   func() bool
		want bool
	}{
		{"URL empty", func() bool { return validators.IsURL("") }, false},
		{"URL plain", func() bool { return validators.IsURL("not a url") }, false},
		{"URL absolute", func() bool { return validators.IsURL("https://example.com") }, true},
		{"duration int", func() bool { return validators.IsDuration(30) }, true},
		{"duration string", func() bool { return validators.IsDuration("30s") }, true},
		{"duration bad string", func() bool { return validators.IsDuration("bad") }, false},
		{"duration wrong type", func() bool { return validators.IsDuration(float64(1)) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigParserParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    time.Duration
		wantErr bool
	}{
		{"int seconds", 45, 45 * time.Second, false},
		{"duration string", "45s", 45 * time.Second, false},
		{"bad string", "not a duration", 0, true},
		{"wrong type", true, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := configParser.ParseDuration(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDuration() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ParseDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTagsUrlToTagKey(t *testing.T) {
	key, err := tags.UrlToTagKey("https://example.com/path")
	if err != nil {
		t.Fatalf("UrlToTagKey(valid) returned error: %v", err)
	}
	if key == "" {
		t.Fatal("UrlToTagKey(valid) returned empty key")
	}
	key2, err := tags.UrlToTagKey("https://example.com/path")
	if err != nil {
		t.Fatalf("second UrlToTagKey(valid) returned error: %v", err)
	}
	if key != key2 {
		t.Fatalf("UrlToTagKey not stable for same URL: %q vs %q", key, key2)
	}
}

func TestDBCommonHelpers(t *testing.T) {
	envTests := []struct {
		name    string
		version string
		want    dbcommon.Environment
	}{
		{"empty", "", dbcommon.Environment("unknown")},
		{"plain", "PostgreSQL 15.2", dbcommon.Environment("self_hosted")},
		{"aurora", "Amazon Aurora MySQL", dbcommon.Environment("aurora")},
		{"rds", "Amazon RDS MySQL", dbcommon.Environment("rds")},
		{"cloudsql", "Google Cloud SQL", dbcommon.Environment("cloudsql")},
		{"azure", "Azure Database for PostgreSQL", dbcommon.Environment("azure_flexible")},
		{"supabase", "Supabase Postgres", dbcommon.Environment("supabase")},
	}
	for _, tt := range envTests {
		t.Run("DetectEnvironment/"+tt.name, func(t *testing.T) {
			if got := dbcommon.DetectEnvironment(tt.version); got != tt.want {
				t.Fatalf("DetectEnvironment(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}

	sysTests := []struct {
		name string
		db   string
		want bool
	}{
		{"mysql", "mysql", true},
		{"performance_schema", "performance_schema", true},
		{"postgres", "postgres", true},
		{"template1", "template1", true},
		{"rdsadmin", "rdsadmin", true},
		{"azure_maintenance", "azure_maintenance", true},
		{"app", "myapp", false},
		{"empty", "", false},
	}
	for _, tt := range sysTests {
		t.Run("IsSystemDatabase/"+tt.name, func(t *testing.T) {
			if got := dbcommon.IsSystemDatabase(tt.db); got != tt.want {
				t.Fatalf("IsSystemDatabase(%q) = %v, want %v", tt.db, got, tt.want)
			}
		})
	}
}

func TestLogparseHelpers(t *testing.T) {
	t.Run("Truncate", func(t *testing.T) {
		if got := logparse.Truncate("abc", 2); got != "ab" {
			t.Fatalf("Truncate(abc, 2) = %q, want %q", got, "ab")
		}
		if got := logparse.Truncate("abc", 0); got != "abc" {
			t.Fatalf("Truncate(abc, 0) = %q, want %q", got, "abc")
		}
		if got := logparse.Truncate("abc", -1); got != "abc" {
			t.Fatalf("Truncate(abc, -1) = %q, want %q", got, "abc")
		}
	})

	t.Run("AssemblerPassThrough", func(t *testing.T) {
		as := logparse.NewAssembler(logparse.MultilineConfig{}, 16)
		got := as.Append("hello")
		if len(got) != 1 || got[0] != "hello" {
			t.Fatalf("Append(hello) = %#v, want [hello]", got)
		}
	})

	t.Run("ParseMultiline", func(t *testing.T) {
		var ml logparse.MultilineConfig
		if err := logparse.ParseMultiline(map[string]interface{}{
			"multiline": map[string]interface{}{"pattern": "^[0-9]{4}"},
		}, &ml); err != nil {
			t.Fatalf("ParseMultiline: %v", err)
		}
		if !ml.Enabled() {
			t.Fatal("expected valid multiline pattern to be enabled")
		}

		var ml2 logparse.MultilineConfig
		if err := logparse.ParseMultiline(map[string]interface{}{}, &ml2); err != nil {
			t.Fatalf("ParseMultiline(missing) error: %v", err)
		}

		var ml3 logparse.MultilineConfig
		if err := logparse.ParseMultiline(map[string]interface{}{
			"multiline": map[string]interface{}{"match": "sideways"},
		}, &ml3); err == nil {
			t.Fatal("expected error for invalid multiline.match")
		}

		var ml4 logparse.MultilineConfig
		if err := logparse.ParseMultiline(map[string]interface{}{
			"multiline": map[string]interface{}{"pattern": "["},
		}, &ml4); err == nil {
			t.Fatal("expected error for invalid multiline.pattern")
		}
	})

	t.Run("ParseParser", func(t *testing.T) {
		var pc logparse.ParserConfig
		if err := logparse.ParseParser(map[string]interface{}{}, &pc); err != nil {
			t.Fatalf("ParseParser(missing) error: %v", err)
		}

		if err := logparse.ParseParser(map[string]interface{}{
			"parser": map[string]interface{}{"type": "nope"},
		}, &pc); err == nil {
			t.Fatal("expected error for unknown parser.type")
		}

		if err := logparse.ParseParser(map[string]interface{}{
			"parser": map[string]interface{}{"type": "regex"},
		}, &pc); err == nil {
			t.Fatal("expected error for regex parser without pattern")
		}

		pc2 := logparse.ParserConfig{}
		if err := logparse.ParseParser(map[string]interface{}{
			"parser": map[string]interface{}{"type": "raw"},
		}, &pc2); err != nil {
			t.Fatalf("ParseParser(raw) error: %v", err)
		}
		if pc2.Type != logparse.ParserRaw {
			t.Fatalf("parser type = %q, want %q", pc2.Type, logparse.ParserRaw)
		}
	})
}

func TestStatePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(state.EnvDir, dir)

	got := state.Path("subdir/sdkcontract.state")
	want := filepath.Join(dir, "sdkcontract.state")
	if got != want {
		t.Fatalf("state.Path() = %q, want %q", got, want)
	}
	if filepath.Base(got) != "sdkcontract.state" {
		t.Fatalf("state.Path() base = %q, want %q", filepath.Base(got), "sdkcontract.state")
	}
	if !strings.HasPrefix(got, dir) {
		t.Fatalf("state.Path() = %q does not start with state dir %q", got, dir)
	}
}

func TestLicenseCatalog(t *testing.T) {
	paid := license.KnownPaidProbes()
	free := license.GetFreeTierProbes()
	if len(paid) == 0 {
		t.Fatal("KnownPaidProbes() is empty")
	}
	if len(free) == 0 {
		t.Fatal("GetFreeTierProbes() is empty")
	}

	freeSet := map[string]bool{}
	for _, name := range free {
		freeSet[name] = true
		if !license.IsProbeAuthorizable(name) {
			t.Fatalf("free tier probe %q is not authorizable", name)
		}
	}
	for _, name := range paid {
		if freeSet[name] {
			t.Fatalf("probe %q is in both paid and free tier lists", name)
		}
		if !license.IsProbeAuthorizable(name) {
			t.Fatalf("paid probe %q is not authorizable", name)
		}
	}
}

type cRegistrationSource struct{}

func (cRegistrationSource) Observe() (entity.Observation, bool) {
	return entity.Observation{}, true
}

func TestEntityRegisterSourceUnregister(t *testing.T) {
	unregister := entity.RegisterSource(cRegistrationSource{})
	if unregister == nil {
		t.Fatal("entity.RegisterSource returned nil unregister func")
	}
	unregister()
}
