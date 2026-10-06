package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"senhub-agent.go/internal/cliexit"
)

func decodeInstallReport(t *testing.T, raw []byte) installReport {
	t.Helper()
	var report installReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("install --json is not one JSON document: %v\n%s", err, raw)
	}
	return report
}

func TestInstallJSONUnchanged(t *testing.T) {
	var out bytes.Buffer
	run := &installRun{json: true, out: &out, configPath: "/etc/senhub-agent/agent.yaml"}

	if code := run.finish(cliexit.Unchanged, nil); code != cliexit.Unchanged {
		t.Fatalf("finish returned %d, want the unchanged code", code)
	}
	report := decodeInstallReport(t, out.Bytes())
	if report.Status != "unchanged" || report.ExitCode != cliexit.Unchanged || !report.OK {
		t.Errorf("header = %+v", report.jsonHeader)
	}
	if report.Changed || report.Written == nil || len(report.Written) != 0 {
		t.Errorf("changed = %v, written = %#v; want false and an empty list", report.Changed, report.Written)
	}
	if report.Schema != "senhub.cli.install/v1" || report.ConfigPath != "/etc/senhub-agent/agent.yaml" {
		t.Errorf("schema/config_path = %q / %q", report.Schema, report.ConfigPath)
	}
}

func TestInstallJSONChangedListsWhatWasWritten(t *testing.T) {
	var out bytes.Buffer
	run := &installRun{json: true, out: &out}
	run.add("/usr/local/bin/senhub-agent")
	run.add("/etc/senhub-agent/agent.yaml", "/etc/senhub-agent/strategies.d/00-http.yaml")

	run.finish(cliexit.OK, nil)
	report := decodeInstallReport(t, out.Bytes())
	if report.Status != "ok" || !report.Changed || len(report.Written) != 3 {
		t.Errorf("report = %+v", report)
	}
}

func TestInstallJSONFailureCarriesTheError(t *testing.T) {
	var out bytes.Buffer
	run := &installRun{json: true, out: &out}

	run.finish(cliexit.Failure, errors.New("boom"))
	report := decodeInstallReport(t, out.Bytes())
	if report.OK || report.Status != "failure" || report.Error != "boom" {
		t.Errorf("report = %+v", report)
	}
}

func TestInstallTextModePrintsNothingExtra(t *testing.T) {
	var out bytes.Buffer
	run := &installRun{out: &out}
	run.add("x")
	if code := run.finish(cliexit.OK, nil); code != cliexit.OK || out.Len() != 0 {
		t.Errorf("text mode wrote %q, code %d", out.String(), code)
	}
}

func TestSnapshotDeltaNamesNewAndChangedFiles(t *testing.T) {
	before := map[string]string{"a": "1:1", "b": "1:1"}
	after := map[string]string{"a": "1:1", "b": "2:2", "c": "1:1"}
	got := snapshotDelta(before, after)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("delta = %v, want [b c]", got)
	}
	if got := snapshotDelta(after, after); len(got) != 0 {
		t.Errorf("identical snapshots reported %v", got)
	}
}
