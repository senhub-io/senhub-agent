package otlpreceiver

import (
	"strings"
	"testing"
)

// TestParseSignalsRefusesAValueItCannotRead pins the shape that cost a
// client three rounds of "the agent cannot receive logs".
//
// `signals: metrics, traces, logs` without brackets is valid YAML and a
// scalar, not a list. It used to leave the receiver on metrics only,
// silently, while the sender got UNIMPLEMENTED for the other two
// signals — a refusal that looks exactly like a missing feature.
func TestParseSignalsRefusesAValueItCannotRead(t *testing.T) {
	// A lone scalar is still accepted as a one-item list — an operator
	// writing `signals: logs` means one signal — so what must fail is a
	// value that yields no names at all.
	for _, raw := range []interface{}{
		42,
		map[string]interface{}{"metrics": true},
		[]interface{}{1, 2},
	} {
		if _, err := parseSignals(raw); err == nil {
			t.Errorf("%#v was accepted and would silently leave the receiver on metrics only", raw)
		} else if !strings.Contains(err.Error(), "brackets") {
			t.Errorf("%#v: the error must say how to write it, got %q", raw, err)
		}
	}
}

// TestParseSignalsKeepsTheDocumentedDefaults: absent and explicitly
// empty both mean metrics only, and that must not change — installs in
// the field rely on it.
func TestParseSignalsKeepsTheDocumentedDefaults(t *testing.T) {
	for name, raw := range map[string]interface{}{
		"absent":               nil,
		"empty list":           []interface{}{},
		"empty list of string": []string{},
	} {
		got, err := parseSignals(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !got.Metrics || got.Logs || got.Traces {
			t.Errorf("%s: want metrics only, got %+v", name, got)
		}
	}
}

// TestParseSignalsReadsWhatTheOperatorWrote covers the working shapes,
// including the lone scalar and the full list.
func TestParseSignalsReadsWhatTheOperatorWrote(t *testing.T) {
	one, err := parseSignals("logs")
	if err != nil {
		t.Fatalf("a lone name: %v", err)
	}
	if !one.Logs || one.Metrics || one.Traces {
		t.Errorf("a lone name must select exactly it, got %+v", one)
	}

	all, err := parseSignals([]interface{}{"metrics", "traces", "logs"})
	if err != nil {
		t.Fatalf("the full list: %v", err)
	}
	if !all.Metrics || !all.Logs || !all.Traces {
		t.Errorf("want all three, got %+v", all)
	}

	// The list replaces the default rather than adding to it: asking for
	// logs alone turns metrics off, and an operator must be able to see
	// that in the startup line.
	logsOnly, err := parseSignals([]interface{}{"logs"})
	if err != nil {
		t.Fatalf("logs only: %v", err)
	}
	if logsOnly.Metrics {
		t.Error("the list must replace the default, not extend it")
	}
}

// TestParseSignalsExplainsTheMissingBrackets: the scalar is read as one
// name, and quoting it back as unknown is accurate but useless. The
// operator needs to be told what to write.
func TestParseSignalsExplainsTheMissingBrackets(t *testing.T) {
	_, err := parseSignals("metrics, traces, logs")
	if err == nil {
		t.Fatal("a comma-separated scalar must be refused")
	}
	if !strings.Contains(err.Error(), "brackets") {
		t.Errorf("the error must name the cause, got %q", err)
	}
	if !strings.Contains(err.Error(), "[metrics, logs, traces]") {
		t.Errorf("the error must show the shape to write, got %q", err)
	}
}
