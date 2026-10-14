package redis

import (
	"bufio"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func parseTestRESP(t *testing.T, raw string) respValue {
	t.Helper()
	v, err := readRESP(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("readRESP: %v", err)
	}
	return v
}

func bulk(s string) string {
	return "$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n"
}

func respArray(parts ...string) string {
	return "*" + strconv.Itoa(len(parts)) + "\r\n" + strings.Join(parts, "")
}

func TestParseSentinelMastersReply(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []sentinelMasterDetails
	}{
		{
			name: "normal reply",
			raw: respArray(respArray(
				bulk("name"), bulk("mymaster"),
				bulk("ip"), bulk("127.0.0.1"),
				bulk("port"), bulk("6379"),
				bulk("flags"), bulk("master"),
				bulk("quorum"), bulk("2"),
				bulk("num-other-sentinels"), bulk("1"),
				bulk("failover-state"), bulk("none"),
			)),
			want: []sentinelMasterDetails{
				{name: "mymaster", address: "127.0.0.1:6379", quorum: 2, otherSentinels: 1},
			},
		},
		{
			name: "failover in progress via flags",
			raw: respArray(respArray(
				bulk("name"), bulk("mymaster"),
				bulk("ip"), bulk("10.0.0.1"),
				bulk("port"), bulk("6379"),
				bulk("flags"), bulk("failover_in_progress"),
				bulk("quorum"), bulk("3"),
				bulk("num-other-sentinels"), bulk("2"),
				bulk("failover-state"), bulk("none"),
			)),
			want: []sentinelMasterDetails{
				{name: "mymaster", address: "10.0.0.1:6379", quorum: 3, otherSentinels: 2, failoverInProgress: true},
			},
		},
		{
			name: "failover in progress via failover-state",
			raw: respArray(respArray(
				bulk("name"), bulk("mymaster"),
				bulk("ip"), bulk("10.0.0.2"),
				bulk("port"), bulk("6379"),
				bulk("flags"), bulk("master"),
				bulk("quorum"), bulk("2"),
				bulk("num-other-sentinels"), bulk("1"),
				bulk("failover-state"), bulk("waiting-start"),
			)),
			want: []sentinelMasterDetails{
				{name: "mymaster", address: "10.0.0.2:6379", quorum: 2, otherSentinels: 1, failoverInProgress: true},
			},
		},
		{
			name: "missing fields",
			raw: respArray(respArray(
				bulk("name"), bulk("mymaster"),
			)),
			want: []sentinelMasterDetails{
				{name: "mymaster"},
			},
		},
		{
			name: "two masters",
			raw: respArray(
				respArray(
					bulk("name"), bulk("primary"),
					bulk("ip"), bulk("10.0.0.1"),
					bulk("port"), bulk("6379"),
					bulk("flags"), bulk("master"),
					bulk("quorum"), bulk("2"),
					bulk("num-other-sentinels"), bulk("1"),
					bulk("failover-state"), bulk("none"),
				),
				respArray(
					bulk("name"), bulk("secondary"),
					bulk("ip"), bulk("10.0.0.2"),
					bulk("port"), bulk("6379"),
					bulk("flags"), bulk("failover_in_progress"),
					bulk("quorum"), bulk("3"),
					bulk("num-other-sentinels"), bulk("2"),
					bulk("failover-state"), bulk("none"),
				),
			),
			want: []sentinelMasterDetails{
				{name: "primary", address: "10.0.0.1:6379", quorum: 2, otherSentinels: 1},
				{name: "secondary", address: "10.0.0.2:6379", quorum: 3, otherSentinels: 2, failoverInProgress: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSentinelMastersReply(parseTestRESP(t, tt.raw))
			if err != nil {
				t.Fatalf("parseSentinelMastersReply: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAuthCommandArgs(t *testing.T) {
	tests := []struct {
		name string
		cfg  probeConfig
		want []string
	}{
		{name: "no password", cfg: probeConfig{}, want: nil},
		{name: "password only", cfg: probeConfig{Password: "secret"}, want: []string{"AUTH", "secret"}},
		{name: "username and password", cfg: probeConfig{Username: "alice", Password: "secret"}, want: []string{"AUTH", "alice", "secret"}},
		{name: "username without password", cfg: probeConfig{Username: "alice"}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := authCommandArgs(tt.cfg)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("authCommandArgs(%+v) = %#v, want %#v", tt.cfg, got, tt.want)
			}
		})
	}
}

func TestParseConfig_Username(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"username": "alice",
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Username != "alice" {
		t.Fatalf("Username = %q, want alice", cfg.Username)
	}

	cfg, err = parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig empty: %v", err)
	}
	if cfg.Username != "" {
		t.Fatalf("Username = %q, want empty", cfg.Username)
	}
}
