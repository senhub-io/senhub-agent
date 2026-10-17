package opamp

import (
	"bytes"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestBuildEffectiveConfig(t *testing.T) {
	files := []configuration.RedactedFile{
		{Name: "agent.yaml", Body: []byte("a: 1")},
		{Name: "probes.d/x.yaml", Body: []byte("b: 2")},
	}

	ec := buildEffectiveConfig(files)
	if ec.GetConfigMap() == nil {
		t.Fatal("config map is nil")
	}

	m := ec.GetConfigMap().GetConfigMap()
	if len(m) != 2 {
		t.Fatalf("file count = %d, want 2", len(m))
	}

	agentFile, ok := m["agent.yaml"]
	if !ok {
		t.Fatal("agent.yaml not found")
	}
	if agentFile.GetContentType() != "text/yaml" {
		t.Fatalf("agent.yaml content type = %q, want text/yaml", agentFile.GetContentType())
	}
	if !bytes.Equal(agentFile.GetBody(), []byte("a: 1")) {
		t.Fatalf("agent.yaml body = %q, want %q", agentFile.GetBody(), "a: 1")
	}

	probeFile, ok := m["probes.d/x.yaml"]
	if !ok {
		t.Fatal("probes.d/x.yaml not found")
	}
	if probeFile.GetContentType() != "text/yaml" {
		t.Fatalf("probe file content type = %q, want text/yaml", probeFile.GetContentType())
	}
	if !bytes.Equal(probeFile.GetBody(), []byte("b: 2")) {
		t.Fatalf("probe file body = %q, want %q", probeFile.GetBody(), "b: 2")
	}

	wantSize := len("a: 1") + len("b: 2")
	if got := effectiveConfigSize(ec); got != wantSize {
		t.Fatalf("effectiveConfigSize = %d, want %d", got, wantSize)
	}
}

func TestBuildEffectiveConfigLargeBodyIsNotTruncated(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 2<<20)
	ec := buildEffectiveConfig([]configuration.RedactedFile{
		{Name: "big.yaml", Body: body},
	})

	m := ec.GetConfigMap().GetConfigMap()
	file, ok := m["big.yaml"]
	if !ok {
		t.Fatal("big.yaml not found")
	}
	if len(file.GetBody()) != 2<<20 {
		t.Fatalf("big.yaml body length = %d, want %d", len(file.GetBody()), 2<<20)
	}
	if got := effectiveConfigSize(ec); got != 2<<20 {
		t.Fatalf("effectiveConfigSize = %d, want %d", got, 2<<20)
	}
}
