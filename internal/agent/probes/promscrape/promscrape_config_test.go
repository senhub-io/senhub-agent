package promscrape

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
		want   scrapeConfig
	}{
		{
			name:   "defaults",
			config: map[string]interface{}{"targets": []string{"http://exporter:9100/metrics"}},
			want:   scrapeConfig{Targets: []string{"http://exporter:9100/metrics"}, Interval: defaultInterval, Timeout: defaultTimeout},
		},
		{
			name: "every option",
			config: map[string]interface{}{
				"targets":              []interface{}{"https://a:9100/metrics", "https://b:9100/metrics"},
				"interval":             15,
				"timeout":              4,
				"insecure_skip_verify": true,
				"bearer_token":         "tok",
				"metric_match":         "^node_",
			},
			want: scrapeConfig{
				Targets:            []string{"https://a:9100/metrics", "https://b:9100/metrics"},
				Interval:           15 * time.Second,
				Timeout:            4 * time.Second,
				InsecureSkipVerify: true,
				BearerToken:        "tok",
				MetricMatch:        "^node_",
			},
		},
		{
			name:   "non-positive interval and timeout keep defaults",
			config: map[string]interface{}{"targets": []string{"http://x/metrics"}, "interval": 0, "timeout": -3},
			want:   scrapeConfig{Targets: []string{"http://x/metrics"}, Interval: defaultInterval, Timeout: defaultTimeout},
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
		"targets not a list":   {"targets": "http://x/metrics"},
		"empty string target":  {"targets": []interface{}{"http://x/metrics", ""}},
		"non-string target":    {"targets": []interface{}{42}},
		"empty string slice":   {"targets": []string{}},
		"invalid metric regex": {"targets": []string{"http://x/metrics"}, "metric_match": "("},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPromScrapeProbe(config, testBaseLogger()); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

func TestPromScrapeProbe_LifecycleAccessors(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"targets":  []interface{}{"http://localhost:9100/metrics"},
		"interval": 45,
	})
	if got, want := p.GetInterval(), 45*time.Second; got != want {
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
