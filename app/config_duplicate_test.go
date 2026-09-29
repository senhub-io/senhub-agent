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
