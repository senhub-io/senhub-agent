package http

import (
	"strings"
	"testing"
)

func TestDashboardSeriesCardNamesTheMissingOTLPOutput(t *testing.T) {
	src, err := jsFiles.ReadFile("assets/js/dashboard.js")
	if err != nil {
		t.Fatalf("reading assets/js/dashboard.js: %v", err)
	}
	code := string(src)
	for _, want := range []string{
		"'senhub.agent.series.untracked'",
		"Series are counted on OTLP outputs only, and none has exported in the last hour.",
		"Series metering is part of the Enterprise edition.",
	} {
		if !strings.Contains(code, want) {
			t.Fatalf("dashboard.js is missing %q", want)
		}
	}
}
