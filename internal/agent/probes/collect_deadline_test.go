package probes

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestCollectDeadline(t *testing.T) {
	cases := []struct {
		name               string
		override, interval time.Duration
		want               time.Duration
	}{
		{"short interval gets the five minute floor", 0, 30 * time.Second, 5 * time.Minute},
		{"interval above the floor is the deadline", 0, 10 * time.Minute, 10 * time.Minute},
		{"override shorter than the interval", 90 * time.Second, 10 * time.Minute, 90 * time.Second},
		{"override longer than the floor", 20 * time.Minute, 30 * time.Second, 20 * time.Minute},
		{"no interval, no override", 0, 0, 0},
		{"no interval, override", 5 * time.Second, 0, 5 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := collectDeadline(c.override, c.interval); got != c.want {
				t.Errorf("collectDeadline(%s, %s) = %s, want %s", c.override, c.interval, got, c.want)
			}
		})
	}
}

func TestCollectTimeoutOf(t *testing.T) {
	cases := []struct {
		name    string
		raw     interface{}
		want    time.Duration
		wantErr bool
	}{
		{"absent", nil, 0, false},
		{"duration string", "90s", 90 * time.Second, false},
		{"number of seconds", 90, 90 * time.Second, false},
		{"float seconds", 1.5, 1500 * time.Millisecond, false},
		{"numeric string", "90", 90 * time.Second, false},
		{"zero means unset", 0, 0, false},
		{"unparsable", "soon", 0, true},
		{"wrong type", true, 0, true},
		{"negative number", -5, 0, true},
		{"negative duration", "-5s", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := CollectTimeoutOf(configuration.ProbeConfig{Name: "p", CollectTimeout: c.raw})
			if (err != nil) != c.wantErr {
				t.Fatalf("error = %v, want error %v", err, c.wantErr)
			}
			if err != nil && (!strings.Contains(err.Error(), "collect_timeout") || !strings.Contains(err.Error(), `"p"`)) {
				t.Errorf("error %q must name the probe and the key", err)
			}
			if got != c.want {
				t.Errorf("CollectTimeoutOf = %s, want %s", got, c.want)
			}
		})
	}
}

func TestCollectTimeoutDecodesFromYAML(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"collect_timeout: 90s": 90 * time.Second,
		"collect_timeout: 90":  90 * time.Second,
	} {
		var p configuration.ProbeConfig
		if err := yaml.Unmarshal([]byte("name: p\ntype: cpu\n"+in+"\n"), &p); err != nil {
			t.Fatal(err)
		}
		got, err := CollectTimeoutOf(p)
		if err != nil || got != want {
			t.Errorf("%q: got %s, %v, want %s", in, got, err, want)
		}
	}
}

func TestCollectTimeoutChangesTheProbeID(t *testing.T) {
	a := configuration.ProbeConfig{Name: "p", Type: "cpu"}
	b := a
	b.CollectTimeout = "90s"
	if a.ID() == b.ID() {
		t.Error("changing collect_timeout must restart the probe: the ID is unchanged")
	}
}
