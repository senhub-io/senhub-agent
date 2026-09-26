package app

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEnvironmentFile(t *testing.T) {
	got := parseEnvironmentFile(strings.NewReader(`# bearer for the otlp output
OTLP_BEARER_TOKEN=abc123
; another comment
QUOTED="with space"
SINGLE='x=y'

not an assignment
`))
	want := map[string]string{"OTLP_BEARER_TOKEN": "abc123", "QUOTED": "with space", "SINGLE": "x=y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseEnvironmentFile = %v, want %v", got, want)
	}
}

func TestParseSystemctlEnvironment(t *testing.T) {
	inline, files := parseSystemctlEnvironment(`Environment=A=1 "B=two words"
EnvironmentFiles=/etc/senhub-agent/bearer.env (ignore_errors=no)
EnvironmentFiles=/etc/senhub-agent/extra.env (ignore_errors=yes)
`)
	if want := map[string]string{"A": "1", "B": "two words"}; !reflect.DeepEqual(inline, want) {
		t.Errorf("inline = %v, want %v", inline, want)
	}
	want := []envFile{{Path: "/etc/senhub-agent/bearer.env"}, {Path: "/etc/senhub-agent/extra.env", Optional: true}}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
}
