package prometheus

import (
	"bytes"
	"strings"
	"testing"
)

func TestTargetInfoCarriesTheResource(t *testing.T) {
	var buf bytes.Buffer
	err := writeTargetInfo(&buf, map[string]string{
		"host.id":             "8b861704-9d1f-4c2a-9f3e-2a1c0d5e7b44",
		"host.name":           "preprod-shop",
		"service.instance.id": "agent-instance-1",
		"service.name":        "senhub-agent",
	})
	if err != nil {
		t.Fatalf("writeTargetInfo: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "# TYPE target_info gauge") {
		t.Errorf("missing TYPE header:\n%s", out)
	}
	// Dots become underscores, as the OTel Prometheus exporters do, so a join
	// written against any other OTel-sourced target_info still works.
	for _, want := range []string{
		`host_id="8b861704-9d1f-4c2a-9f3e-2a1c0d5e7b44"`,
		`host_name="preprod-shop"`,
		`service_instance_id="agent-instance-1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "} 1") {
		t.Errorf("target_info must carry the value 1:\n%s", out)
	}
}

// An attribute the agent could not resolve is omitted rather than exported
// empty: host_id="" joins to nothing and reads as an identity rather than as
// an absence.
func TestTargetInfoOmitsEmptyAttributes(t *testing.T) {
	var buf bytes.Buffer
	if err := writeTargetInfo(&buf, map[string]string{"host.id": "", "host.name": "shop"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "host_id") {
		t.Errorf("an unresolved attribute must not be exported:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `host_name="shop"`) {
		t.Errorf("the resolved attributes must still be exported:\n%s", buf.String())
	}
}

// No resource at all: say nothing. An empty target_info would match a join on
// nothing while looking like it should.
func TestTargetInfoSilentWithoutAResource(t *testing.T) {
	for _, res := range []map[string]string{nil, {}, {"host.id": ""}} {
		var buf bytes.Buffer
		if err := writeTargetInfo(&buf, res); err != nil {
			t.Fatal(err)
		}
		if buf.Len() != 0 {
			t.Errorf("expected no output for %v, got:\n%s", res, buf.String())
		}
	}
}

func TestTargetInfoEscapesLabelValues(t *testing.T) {
	var buf bytes.Buffer
	if err := writeTargetInfo(&buf, map[string]string{"host.name": `a"b\c`}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `host_name="a\"b\\c"`) {
		t.Errorf("quote and backslash must be escaped per the text format:\n%s", buf.String())
	}
}

// The exposition must lead with target_info, before any metric group, so a
// reader sees whose metrics these are first.
func TestSerializeEmitsTargetInfoFirst(t *testing.T) {
	var buf bytes.Buffer
	err := SerializeToTextExposition(nil, &buf, SerializeOptions{
		Resource: map[string]string{"host.id": "abc"},
	})
	if err != nil {
		t.Fatalf("SerializeToTextExposition: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "# HELP target_info") {
		t.Errorf("target_info must come first:\n%s", buf.String())
	}
}

// And no target_info when the caller declares no resource — the previous
// behaviour, unchanged for every existing caller.
func TestSerializeWithoutResourceIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	if err := SerializeToTextExposition(nil, &buf, SerializeOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "target_info") {
		t.Errorf("no resource declared, yet target_info was emitted:\n%s", buf.String())
	}
}
