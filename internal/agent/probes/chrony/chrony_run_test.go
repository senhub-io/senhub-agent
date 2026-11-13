package chrony

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

const runChronycTrackingOutput = "65D8456C,2620:2d:4000:1::3123,3,1786708478.062514584,0.000106370,-0.000040664,0.000311689,0.647,-0.001,0.099,0.015753603,0.000998904,1026.5,Normal"

func newRunTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func writeFakeChronycScript(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake chronyc uses a POSIX shell script")
	}
	script := fmt.Sprintf("#!/bin/sh\ncat <<'EOF'\n%s\nEOF\nexit %d\n", stdout, exitCode)
	path := filepath.Join(t.TempDir(), "fake_chronyc.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake chronyc script: %v", err)
	}
	return path
}

func replaceTrackingFieldForRunTest(t *testing.T, idx int, value string) string {
	t.Helper()
	fields := strings.Split(runChronycTrackingOutput, ",")
	if idx < 0 || idx >= len(fields) {
		t.Fatalf("field index %d out of range for %d fields", idx, len(fields))
	}
	fields[idx] = value
	return strings.Join(fields, ",")
}

func TestChronyProbeGetTargetStrategies_ReturnsKnownSinks(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newRunTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)

	got := p.GetTargetStrategies()
	want := []string{"senhub", "prtg", "http", "otlp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetTargetStrategies = %v, want %v", got, want)
	}
}

func TestChronyProbeOnStart_ReturnsNil(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newRunTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)

	if err := p.OnStart(nil); err != nil {
		t.Errorf("OnStart returned error: %v", err)
	}
}

func TestChronyProbeOnShutdown_ReturnsNil(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newRunTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)

	if err := p.OnShutdown(context.Background()); err != nil {
		t.Errorf("OnShutdown returned error: %v", err)
	}
}

func TestParseTracking_NumericFieldErrorsNameFieldAndIndex(t *testing.T) {
	cases := []struct {
		name string
		idx  int
	}{
		{"stratum", fieldStratum},
		{"system_time", fieldSystemTime},
		{"freq_ppm", fieldFreqPPM},
		{"skew", fieldSkew},
		{"root_delay", fieldRootDelay},
		{"root_dispersion", fieldRootDispersion},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := replaceTrackingFieldForRunTest(t, tc.idx, "not-a-number")
			res := parseTracking(line)
			if res.err == nil {
				t.Fatal("expected a numeric parse error, got nil")
			}

			want := fmt.Sprintf("parsing %s from field %d (%q)", tc.name, tc.idx, "not-a-number")
			if !strings.Contains(res.err.Error(), want) {
				t.Errorf("error %q does not contain %q", res.err, want)
			}
		})
	}
}

func TestParseTracking_TooFewFieldsReportsCount(t *testing.T) {
	res := parseTracking("a,b")
	if res.err == nil {
		t.Fatal("expected error for a short line, got nil")
	}

	want := `chronyc tracking: expected 14 fields, got 2 (line: "a,b")`
	if res.err.Error() != want {
		t.Errorf("error = %q, want %q", res.err, want)
	}
}

func TestParseTracking_TrimsLeapStatusWhitespace(t *testing.T) {
	line := replaceTrackingFieldForRunTest(t, fieldLeapStatus, "  Normal  ")
	res := parseTracking(line)
	if res.err != nil {
		t.Fatalf("parseTracking with padded leap status: %v", res.err)
	}
	if res.leapStatus != "Normal" {
		t.Errorf("leapStatus = %q, want Normal", res.leapStatus)
	}
}

func TestCappedWriter_RespectsLimitAndReportsLen(t *testing.T) {
	cases := []struct {
		name       string
		max        int
		initial    string
		input      string
		wantStored string
	}{
		{"writes all when below limit", 10, "", "abc", "abc"},
		{"writes only up to limit in one write", 4, "", "abcdef", "abcd"},
		{"writes nothing once buffer is full", 4, "abcd", "efgh", "abcd"},
		{"writes remaining bytes across boundary", 5, "abc", "defgh", "abcde"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.NewBufferString(tc.initial)
			w := &cappedWriter{buf: buf, max: tc.max}

			n, err := w.Write([]byte(tc.input))
			if err != nil {
				t.Fatalf("Write returned error: %v", err)
			}
			if n != len(tc.input) {
				t.Errorf("Write returned %d, want %d", n, len(tc.input))
			}
			if got := buf.String(); got != tc.wantStored {
				t.Errorf("stored = %q, want %q", got, tc.wantStored)
			}
		})
	}
}

func TestRunOnce_ExecutesConfiguredChronycAndParsesTracking(t *testing.T) {
	script := writeFakeChronycScript(t, runChronycTrackingOutput, 0)
	p := &ChronyProbe{cfg: chronyConfig{ChronyPath: script}}

	res := p.runOnce()
	if res.err != nil {
		t.Fatalf("runOnce on valid chronyc output: %v", res.err)
	}
	if res.stratum != 3 {
		t.Errorf("stratum = %v, want 3", res.stratum)
	}
	if res.leapStatus != "Normal" {
		t.Errorf("leapStatus = %q, want Normal", res.leapStatus)
	}
}

func TestRunOnce_ScriptExitNonZeroWrapsChronyc(t *testing.T) {
	script := writeFakeChronycScript(t, "", 1)
	p := &ChronyProbe{cfg: chronyConfig{ChronyPath: script}}

	res := p.runOnce()
	if res.err == nil {
		t.Fatal("expected an error from non-zero chronyc exit")
	}
	if !strings.HasPrefix(res.err.Error(), "chronyc:") {
		t.Errorf("error %q does not start with chronyc:", res.err)
	}
	if !strings.Contains(res.err.Error(), "exit status 1") {
		t.Errorf("error %q does not mention exit status 1", res.err)
	}
}

func TestRunOnce_NonexistentPathWrapsChronyc(t *testing.T) {
	p := &ChronyProbe{cfg: chronyConfig{ChronyPath: filepath.Join(t.TempDir(), "missing-chronyc")}}

	res := p.runOnce()
	if res.err == nil {
		t.Fatal("expected an error from nonexistent chronyc path")
	}
	if !strings.HasPrefix(res.err.Error(), "chronyc:") {
		t.Errorf("error %q does not start with chronyc:", res.err)
	}
}

func TestRunOnce_LargeOutputIsTruncatedBeforeParsing(t *testing.T) {
	fields := strings.Split(runChronycTrackingOutput, ",")
	// A huge first CSV field makes the 4 KiB cut land before the first comma,
	// so a full, untruncated line would parse but the capped output cannot.
	fields[0] = strings.Repeat("X", maxOutputBytes*2)
	hugeLine := strings.Join(fields, ",")

	script := writeFakeChronycScript(t, hugeLine, 0)
	p := &ChronyProbe{cfg: chronyConfig{ChronyPath: script}}

	res := p.runOnce()
	if res.err == nil {
		t.Fatal("expected a parse error from truncated output, got nil")
	}
	if !strings.Contains(res.err.Error(), "expected 14 fields, got 1") {
		t.Errorf("truncated error = %q, want field-count parse error", res.err)
	}
}

func TestChronyEntitySourceObserve_ReachabilityVariants(t *testing.T) {
	cases := []struct {
		name           string
		up             bool
		version        string
		hostID         string
		wantObserved   bool
		wantVersion    string
		wantVersionSet bool
	}{
		{"not up hides entity", false, "", "machine-1", false, "", false},
		{"up with empty host id hides entity", true, "", "", false, "", false},
		{"up with host id and no version emits no version attribute", true, "", "machine-1", true, "", false},
		{"up with host id and version emits version attribute", true, "4.5", "machine-1", true, "4.5", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newChronyEntitySource()
			src.setReachable(tc.up, tc.version, tc.hostID)

			obs, ok := src.Observe()
			if ok != tc.wantObserved {
				t.Fatalf("Observe ok = %v, want %v", ok, tc.wantObserved)
			}
			if !tc.wantObserved {
				if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
					t.Fatalf("Observe returned an observation despite ok=false: %+v", obs)
				}
				return
			}

			if len(obs.Entities) != 1 {
				t.Fatalf("Entities = %d, want 1", len(obs.Entities))
			}
			ent := obs.Entities[0]
			if ent.Type != "service.instance" {
				t.Errorf("Entity.Type = %q, want service.instance", ent.Type)
			}

			wantID := map[string]any{"service.instance.id": "chrony@" + tc.hostID}
			if !reflect.DeepEqual(ent.ID, wantID) {
				t.Errorf("Entity.ID = %#v, want %#v", ent.ID, wantID)
			}
			if got := ent.Attributes["service.name"]; got != "chrony" {
				t.Errorf("service.name = %v, want chrony", got)
			}
			if got := ent.Attributes["server.address"]; got != "localhost" {
				t.Errorf("server.address = %v, want localhost", got)
			}

			gotVersion, versionSet := ent.Attributes["service.version"]
			if versionSet != tc.wantVersionSet {
				t.Errorf("service.version set = %v, want %v", versionSet, tc.wantVersionSet)
			}
			if versionSet && gotVersion != tc.wantVersion {
				t.Errorf("service.version = %v, want %q", gotVersion, tc.wantVersion)
			}

			wantToID := map[string]any{"host.id": tc.hostID}
			foundRunsOn := false
			for _, rel := range obs.Relations {
				if rel.Type != "runs_on" {
					continue
				}
				foundRunsOn = true
				if rel.FromType != "service.instance" {
					t.Errorf("runs_on FromType = %q, want service.instance", rel.FromType)
				}
				if !reflect.DeepEqual(rel.FromID, wantID) {
					t.Errorf("runs_on FromID = %#v, want %#v", rel.FromID, wantID)
				}
				if rel.ToType != "host" {
					t.Errorf("runs_on ToType = %q, want host", rel.ToType)
				}
				if !reflect.DeepEqual(rel.ToID, wantToID) {
					t.Errorf("runs_on ToID = %#v, want %#v", rel.ToID, wantToID)
				}
			}
			if !foundRunsOn {
				t.Errorf("runs_on relation not found; got %+v", obs.Relations)
			}
		})
	}
}
