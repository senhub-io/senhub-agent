package opamp

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/open-telemetry/opamp-go/protobufs"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
)

func TestBuildDescriptionIdentifyingAttributes(t *testing.T) {
	desc := buildDescription(descriptionInput{
		InstanceID:  "00000000-0000-0000-0000-000000000001",
		Version:     "1.2.3",
		Host:        map[string]string{"host.id": "host-123"},
		Edition:     "oss",
		LicenseTier: "free",
	})

	identifying := desc.GetIdentifyingAttributes()
	if len(identifying) != 3 {
		t.Fatalf("identifying attributes = %d, want 3", len(identifying))
	}

	want := []struct {
		key   string
		value string
	}{
		{"service.name", ServiceName},
		{"service.instance.id", "00000000-0000-0000-0000-000000000001"},
		{"service.version", "1.2.3"},
	}
	for i, w := range want {
		kv := identifying[i]
		if kv.GetKey() != w.key {
			t.Fatalf("identifying[%d] key = %q, want %q", i, kv.GetKey(), w.key)
		}
		if got := kv.GetValue().GetStringValue(); got != w.value {
			t.Fatalf("identifying[%d] value = %q, want %q", i, got, w.value)
		}
	}
}

func TestBuildDescriptionNonIdentifyingAttributes(t *testing.T) {
	const agentKey = "agent-key-should-not-appear"

	desc := buildDescription(descriptionInput{
		InstanceID: "00000000-0000-0000-0000-000000000001",
		Version:    "1.0.0",
		Host: map[string]string{
			"host.id":        "ID-With.Case_0",
			"host.name":      "host-1",
			"os.type":        "linux",
			"os.description": "Debian",
			"host.arch":      "x86_64",
		},
		Edition:     "enterprise",
		LicenseTier: "pro",
	})

	attrs := desc.GetNonIdentifyingAttributes()
	var keys []string
	var values []string
	for _, kv := range attrs {
		keys = append(keys, kv.GetKey())
		values = append(values, kv.GetValue().GetStringValue())
	}

	wantKeys := []string{
		"host.id",
		"host.name",
		"os.type",
		"os.description",
		"host.arch",
		"senhub.agent.edition",
		"senhub.license.tier",
		"senhub.agent.config.source",
	}
	if len(keys) != len(wantKeys) {
		t.Fatalf("non-identifying keys = %v, want %v", keys, wantKeys)
	}
	for i := range wantKeys {
		if keys[i] != wantKeys[i] {
			t.Fatalf("non-identifying[%d] = %q, want %q", i, keys[i], wantKeys[i])
		}
	}

	if got := attrs[len(attrs)-1].GetValue().GetStringValue(); got != ConfigSourceLocal {
		t.Fatalf("last non-identifying value = %q, want %q", got, ConfigSourceLocal)
	}
	if got := attrs[0].GetValue().GetStringValue(); got != "ID-With.Case_0" {
		t.Fatalf("host.id = %q, want %q", got, "ID-With.Case_0")
	}

	for _, key := range keys {
		if strings.Contains(strings.ToLower(key), "subject") {
			t.Fatalf("unexpected license subject key %q", key)
		}
	}
	for _, value := range values {
		if value == agentKey {
			t.Fatalf("agent key leaked into description attributes")
		}
	}
}

func TestBuildDescriptionSkipsEmptyHostAttributes(t *testing.T) {
	desc := buildDescription(descriptionInput{
		InstanceID: "00000000-0000-0000-0000-000000000001",
		Version:    "1.0.0",
		Host: map[string]string{
			"host.id": "",
			"os.type": "linux",
		},
	})

	m := make(map[string]string)
	for _, kv := range desc.GetNonIdentifyingAttributes() {
		m[kv.GetKey()] = kv.GetValue().GetStringValue()
	}

	if _, ok := m["host.id"]; ok {
		t.Fatal("empty host.id was not skipped")
	}
	if m["os.type"] != "linux" {
		t.Fatalf("os.type = %q, want linux", m["os.type"])
	}
	if m["senhub.agent.config.source"] != ConfigSourceLocal {
		t.Fatalf("config source = %q, want %q", m["senhub.agent.config.source"], ConfigSourceLocal)
	}
}

func TestDescriptionHostIDMatchesHostIdentity(t *testing.T) {
	attrs, err := common.GetHostResourceAttributes()
	if err != nil {
		t.Skipf("host resource attributes unavailable: %v", err)
	}
	identity, err := common.GetHostIdentity()
	if err != nil {
		t.Skipf("host identity unavailable: %v", err)
	}
	if attrs["host.id"] == "" {
		t.Skip("host.id resource attribute is empty")
	}

	desc := buildDescription(descriptionInput{
		InstanceID: "00000000-0000-0000-0000-000000000001",
		Version:    "1.0.0",
		Host:       attrs,
	})

	value, ok := attributeValue(desc.GetNonIdentifyingAttributes(), "host.id")
	if !ok {
		t.Fatal("host.id not present in non-identifying attributes")
	}
	if value != attrs["host.id"] {
		t.Fatalf("host.id = %q, resource attribute = %q", value, attrs["host.id"])
	}
	if value != identity.ID {
		t.Fatalf("host.id = %q, host identity ID = %q", value, identity.ID)
	}
}

func TestInstanceUID(t *testing.T) {
	id := configuration.AgentInstanceID("k1")
	if id == "" {
		t.Fatal("AgentInstanceID returned empty")
	}

	got, err := instanceUID(id)
	if err != nil {
		t.Fatalf("instanceUID(%q) error: %v", id, err)
	}
	want, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("uuid.Parse(%q) error: %v", id, err)
	}
	if !bytes.Equal(got[:], want[:]) {
		t.Fatalf("instanceUID = %x, want %x", got[:], want[:])
	}

	if _, err := instanceUID(""); err == nil {
		t.Fatal("instanceUID(\"\") expected an error")
	}
	if _, err := instanceUID("not-a-uuid"); err == nil {
		t.Fatal("instanceUID(\"not-a-uuid\") expected an error")
	}
}

func TestEdition(t *testing.T) {
	paid := license.KnownPaidProbes()
	if len(paid) == 0 {
		t.Skip("no paid probes configured")
	}

	if got := Edition(func(string) bool { return true }); got != "enterprise" {
		t.Fatalf("Edition(all true) = %q, want enterprise", got)
	}
	if got := Edition(func(string) bool { return false }); got != "oss" {
		t.Fatalf("Edition(all false) = %q, want oss", got)
	}
	if got := Edition(nil); got != "" {
		t.Fatalf("Edition(nil) = %q, want empty", got)
	}

	for _, probeType := range paid {
		if probeType == "veeam" {
			got := Edition(func(name string) bool { return name == "veeam" })
			if got != "enterprise" {
				t.Fatalf("Edition(veeam only) = %q, want enterprise", got)
			}
		}
	}
}

type fakeValidator struct {
	lic     *license.License
	err     error
	inGrace bool
}

func (f fakeValidator) ValidateLicense(string) (*license.License, error) {
	return f.lic, f.err
}

func (f fakeValidator) IsProbeAuthorized(*license.License, string) bool {
	return true
}

func (f fakeValidator) IsInGracePeriod(*license.License) bool {
	return f.inGrace
}

func TestLicenseTier(t *testing.T) {
	if got := licenseTier("", "agent-key", nil); got != "free" {
		t.Fatalf("empty token tier = %q, want free", got)
	}

	if got := licenseTier("token", "agent-key", func() (license.Validator, error) {
		return nil, errors.New("validator unavailable")
	}); got != "" {
		t.Fatalf("validator constructor error tier = %q, want empty", got)
	}

	if got := licenseTier("token", "agent-key", func() (license.Validator, error) {
		return fakeValidator{err: errors.New("invalid")}, nil
	}); got != "free" {
		t.Fatalf("validation error tier = %q, want free", got)
	}

	if got := licenseTier("token", "agent-key", func() (license.Validator, error) {
		return fakeValidator{lic: &license.License{Tier: license.TierPro}}, nil
	}); got != string(license.TierPro) {
		t.Fatalf("valid pro tier = %q, want %q", got, license.TierPro)
	}

	if got := licenseTier("token", "agent-key", func() (license.Validator, error) {
		return fakeValidator{
			lic: &license.License{Tier: license.TierEnterprise, IsExpired: true},
		}, nil
	}); got != "free" {
		t.Fatalf("expired no-grace tier = %q, want free", got)
	}
}

func attributeValue(attrs []*protobufs.KeyValue, key string) (string, bool) {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue(), true
		}
	}
	return "", false
}
