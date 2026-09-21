package transformers

import (
	"strings"
	"testing"
)

// A dimension is part of the identity of a series. It is what tells one
// instance from another on every output: a Zabbix item key, a Prometheus
// label set, an OTLP attribute set. So a dimension whose value changes
// when nothing meaningful changed mints a new series each time and
// orphans the previous one.
//
// The process probe declared the process id as a dimension of six of its
// metrics. On an ordinary machine that grew to 4596 series and added
// about fifty a minute, for ever. Its own entity source had refused to
// do the same thing on the other rail years earlier, and said why.
//
// This is the cheap half of that lesson: the names below are values that
// the operating system reuses or reassigns, and none of them belongs in
// the identity of a series. They may be carried as tags, which is where
// a detail that changes belongs.
var ephemeralDimensions = map[string]string{
	"process.pid":   "a process id is reassigned at every restart",
	"pid":           "a process id is reassigned at every restart",
	"session.id":    "a session identifier lives no longer than the session",
	"connection.id": "a connection identifier lives no longer than the connection",
	"thread.id":     "a thread identifier is reused within a process",
	"request.id":    "a request identifier is unique to one request",
}

// acknowledged lists the places where an ephemeral dimension is the
// point rather than an oversight, with the reason and what bounds it.
// An entry that no longer matches anything fails this test, so the list
// cannot become somewhere a gap hides, the way the documented-key guard
// works.
var acknowledged = map[string]string{
	"process/process.cpu.utilization":             perProcessDetail,
	"process/process.memory.usage":                perProcessDetail,
	"process/process.memory.virtual_memory_usage": perProcessDetail,
	"process/process.threads":                     perProcessDetail,
	"process/process.open_file_descriptors":       perProcessDetail,
	"process/process.uptime":                      perProcessDetail,
}

const perProcessDetail = "per-process monitoring is what this metric is for; " +
	"the probe only emits it once a filter names what to watch or top_n bounds " +
	"the sample, and an unfiltered view reports the per-name roll-up instead"

func TestNoDimensionCarriesAValueTheSystemReassigns(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	used := map[string]bool{}
	for _, def := range defs {
		for _, label := range def.MultiInstanceLabels {
			checked++
			if why, bad := ephemeralDimensions[strings.ToLower(label)]; bad {
				t.Errorf("%s declares %q as a dimension of every metric: %s, so each one mints a new series and orphans the last. Carry it as a tag instead.",
					def.ProbeName, label, why)
			}
		}
		for _, m := range def.Metrics {
			for _, label := range m.MultiInstanceLabels {
				checked++
				why, bad := ephemeralDimensions[strings.ToLower(label)]
				if !bad {
					continue
				}
				id := def.ProbeName + "/" + m.Name
				if _, ok := acknowledged[id]; ok {
					used[id] = true
					continue
				}
				t.Errorf("%s declares %q as a dimension: %s, so each one mints a new series and orphans the last. Carry it as a tag instead, or acknowledge it here with what bounds it.",
					id, label, why)
			}
		}
	}
	for id := range acknowledged {
		if !used[id] {
			t.Errorf("%s is acknowledged here but no longer declares an ephemeral dimension; remove the entry", id)
		}
	}
	if checked < 100 {
		t.Fatalf("only %d dimensions examined; the embedded definitions should give far more", checked)
	}
}
