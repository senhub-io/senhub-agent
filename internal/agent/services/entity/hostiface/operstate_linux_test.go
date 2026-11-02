//go:build linux

package hostiface

import (
	"testing"

	"senhub-agent.go/internal/agent/services/entity/operstate"
)

func TestReadSysLinkOperstateMapping(t *testing.T) {
	cases := map[string]string{
		"up":             "up",
		"down":           "down",
		"testing":        "testing",
		"unknown":        "unknown",
		"dormant":        "dormant",
		"notpresent":     "not_present",
		"lowerlayerdown": "lower_layer_down",
		"":               "",
		"garbage":        "",
	}
	for in, want := range cases {
		got, _ := operstate.FromLinuxSysfs(in)
		if got != want {
			t.Errorf("FromLinuxSysfs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormDuplex(t *testing.T) {
	cases := map[string]string{"full": "full", "half": "half", "unknown": "unknown", "": "", "garbage": ""}
	for in, want := range cases {
		if got := normDuplex(in); got != want {
			t.Errorf("normDuplex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSpeedMbit(t *testing.T) {
	cases := map[string]int64{
		"1000": 1_000_000_000, // 1 Gbit/s
		"100":  100_000_000,
		"-1":   0, // down
		"0":    0,
		"":     0,
		"abc":  0,
	}
	for in, want := range cases {
		if got := parseSpeedMbit(in); got != want {
			t.Errorf("parseSpeedMbit(%q) = %d, want %d", in, got, want)
		}
	}
}
