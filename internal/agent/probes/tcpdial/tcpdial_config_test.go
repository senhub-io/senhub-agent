package tcpdial

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestParseConfig_ValuesAndDefaults(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   checkConfig
	}{
		{
			name:   "defaults",
			config: map[string]interface{}{"targets": []string{"db:5432"}},
			want:   checkConfig{Targets: []string{"db:5432"}, Timeout: defaultTimeout, Interval: defaultInterval},
		},
		{
			name:   "ipv6 literal and options",
			config: map[string]interface{}{"targets": []interface{}{"[::1]:443", "mail:25"}, "timeout": 3, "interval": 20},
			want:   checkConfig{Targets: []string{"[::1]:443", "mail:25"}, Timeout: 3 * time.Second, Interval: 20 * time.Second},
		},
		{
			name:   "non-positive options keep defaults",
			config: map[string]interface{}{"targets": []string{"db:5432"}, "timeout": 0, "interval": -1},
			want:   checkConfig{Targets: []string{"db:5432"}, Timeout: defaultTimeout, Interval: defaultInterval},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConfig(tc.config)
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseConfig() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseConfig_RejectsMalformedTargets(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"targets not a list":  {"targets": "db:5432"},
		"empty string target": {"targets": []interface{}{""}},
		"non-string target":   {"targets": []interface{}{5432}},
		"port missing":        {"targets": []interface{}{"db"}},
		"empty string slice":  {"targets": []string{}},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConfig(config); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

func TestTCPDialProbe_LifecycleAccessors(t *testing.T) {
	p := newTestProbe(t, "127.0.0.1:1")
	if got, want := p.GetInterval(), defaultInterval; got != want {
		t.Errorf("GetInterval() = %v, want %v", got, want)
	}
	if !p.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
	if got, want := p.GetTargetStrategies(), []string{"senhub", "prtg", "http", "otlp"}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetTargetStrategies() = %v, want %v", got, want)
	}
	if err := p.OnStart(nil); err != nil {
		t.Errorf("OnStart() = %v, want nil", err)
	}
	if err := p.OnShutdown(context.Background()); err != nil {
		t.Errorf("OnShutdown() = %v, want nil", err)
	}
}
