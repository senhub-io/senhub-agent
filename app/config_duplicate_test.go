package app

import (
	"reflect"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestDuplicateProbeIndexes(t *testing.T) {
	list := []configuration.ProbeConfig{
		{Name: "cpu", Type: "cpu"},
		{Name: "memory", Type: "memory"},
		{Name: "cpu", Type: "cpu"},
		{Name: "", Type: "cpu"},
		{Name: "", Type: "cpu"},
		{Name: "cpu", Type: "process"},
	}
	want := map[int]bool{2: true, 5: true}
	if got := duplicateProbeIndexes(list); !reflect.DeepEqual(got, want) {
		t.Errorf("duplicateProbeIndexes = %v, want %v", got, want)
	}
}

func TestFiletailOverlaps(t *testing.T) {
	off := false
	list := []configuration.ProbeConfig{
		{Name: "nginx-logs", Type: "filetail", Params: map[string]interface{}{
			"paths": []interface{}{"/var/log/nginx/access.log", "/var/log/nginx/error.log"}}},
		{Name: "platform-logs", Type: "filetail", Params: map[string]interface{}{
			"paths": []interface{}{"/var/log/nginx/../nginx/access.log", "/var/log/app.log"}}},
		{Name: "disabled", Type: "filetail", Enabled: &off, Params: map[string]interface{}{
			"paths": []interface{}{"/var/log/nginx/error.log"}}},
		{Name: "same-twice", Type: "filetail", Params: map[string]interface{}{
			"paths": []string{"/var/log/other.log", "/var/log/other.log"}}},
		{Name: "not-filetail", Type: "linux_logs", Params: map[string]interface{}{
			"paths": []interface{}{"/var/log/app.log"}}},
	}
	want := []pathOverlap{{path: "/var/log/nginx/access.log", probes: []string{`"nginx-logs"`, `"platform-logs"`}}}
	if got := filetailOverlaps(list); !reflect.DeepEqual(got, want) {
		t.Errorf("filetailOverlaps = %v, want %v", got, want)
	}
}
