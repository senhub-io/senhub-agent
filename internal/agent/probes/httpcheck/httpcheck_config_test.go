package httpcheck

import (
	"context"
	"net/http"
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
			config: map[string]interface{}{"targets": []string{"https://example.org/"}},
			want: checkConfig{
				Targets: []string{"https://example.org/"}, Method: http.MethodGet,
				Timeout: defaultTimeout, Interval: defaultInterval, MaxBodyBytes: defaultMaxBodyBytes,
			},
		},
		{
			name: "every option",
			config: map[string]interface{}{
				"targets":              []interface{}{"https://a/", "https://b/health"},
				"method":               "HEAD",
				"timeout":              7,
				"interval":             25,
				"expected_status":      204,
				"content_match":        "ok",
				"insecure_skip_verify": true,
			},
			want: checkConfig{
				Targets: []string{"https://a/", "https://b/health"}, Method: "HEAD",
				Timeout: 7 * time.Second, Interval: 25 * time.Second, MaxBodyBytes: defaultMaxBodyBytes,
				ExpectedStatus: 204, ContentMatch: "ok", InsecureSkipVerify: true,
			},
		},
		{
			name: "empty method and non-positive numbers keep defaults",
			config: map[string]interface{}{
				"targets": []string{"https://a/"}, "method": "", "timeout": 0, "interval": -2, "expected_status": 0,
			},
			want: checkConfig{
				Targets: []string{"https://a/"}, Method: http.MethodGet,
				Timeout: defaultTimeout, Interval: defaultInterval, MaxBodyBytes: defaultMaxBodyBytes,
			},
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
		"targets not a list":  {"targets": "https://a/"},
		"empty string target": {"targets": []interface{}{"https://a/", ""}},
		"non-string target":   {"targets": []interface{}{true}},
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

func TestHTTPCheckProbe_LifecycleAccessors(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"https://a/"}, "interval": 40})
	if got, want := p.GetInterval(), 40*time.Second; got != want {
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
