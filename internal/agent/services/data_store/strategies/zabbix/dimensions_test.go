package zabbix

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// A probe whose metrics are per instance can still carry a value that
// belongs to the machine. The process probe counts processes by name and
// reports the kernel's ceiling on how many may exist; the ceiling is not
// per name. Declaring no list inherits the probe's, declaring an empty
// one says there are no dimensions at all.
func TestAnEmptyDimensionListIsNotTheSameAsNoList(t *testing.T) {
	def := &transformers.ProbeDefinition{
		ProbeName:           "process",
		MultiInstanceLabels: []string{"process.name"},
	}
	inherits := &transformers.MetricDefinition{Name: "counted_per_name"}
	machineWide := &transformers.MetricDefinition{Name: "kernel_ceiling", MultiInstanceLabels: []string{}}
	ownList := &transformers.MetricDefinition{Name: "detailed", MultiInstanceLabels: []string{"process.name", "process.pid"}}

	if got := dimensions(def, inherits); len(got) != 1 || got[0] != "process.name" {
		t.Errorf("a metric declaring no list must inherit the probe's; got %v", got)
	}
	if got := dimensions(def, machineWide); len(got) != 0 {
		t.Errorf("an empty list means no dimensions; got %v, which would key a machine-wide value per instance", got)
	}
	if got := dimensions(def, ownList); len(got) != 2 {
		t.Errorf("a metric's own list replaces the probe's; got %v", got)
	}
}
