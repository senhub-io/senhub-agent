package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMultiFileForSet(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"k\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sd := filepath.Join(dir, "strategies.d")
	if err := os.MkdirAll(sd, 0o750); err != nil {
		t.Fatal(err)
	}
	frag := "# keep me\nhttp:\n  port: 8080\n  bind_address: \"127.0.0.1\"\n  endpoints: [\"prtg\", \"web\"]\n"
	if err := os.WriteFile(filepath.Join(sd, "00-http.yaml"), []byte(frag), 0o600); err != nil {
		t.Fatal(err)
	}
	return main
}

func TestSetStrategyScalar_PortRoundTripsAsInt(t *testing.T) {
	main := writeMultiFileForSet(t)
	if err := SetStrategyScalar(main, "http", "port", "9090", "!!int"); err != nil {
		t.Fatalf("SetStrategyScalar: %v", err)
	}
	frag := filepath.Join(filepath.Dir(main), "strategies.d", "00-http.yaml")
	raw, _ := os.ReadFile(frag)
	got := string(raw)
	if !strings.Contains(got, "# keep me") {
		t.Error("comment was not preserved")
	}
	if !strings.Contains(got, "port: 9090") || strings.Contains(got, "port: \"9090\"") {
		t.Errorf("port must round-trip as an unquoted int:\n%s", got)
	}
	// The loader must read it back as the new port.
	cfg, err := LoadFromDisk(main, nil)
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	for _, s := range cfg.Storage {
		if s.Name == "http" && s.Params["port"] != 9090 {
			t.Errorf("loaded port = %v (%T), want int 9090", s.Params["port"], s.Params["port"])
		}
	}
}

func TestSetStrategyScalar_BindAddressIsString(t *testing.T) {
	main := writeMultiFileForSet(t)
	if err := SetStrategyScalar(main, "http", "bind_address", "0.0.0.0", "!!str"); err != nil {
		t.Fatalf("SetStrategyScalar: %v", err)
	}
	cfg, _ := LoadFromDisk(main, nil)
	for _, s := range cfg.Storage {
		if s.Name == "http" && s.Params["bind_address"] != "0.0.0.0" {
			t.Errorf("bind_address = %v, want 0.0.0.0", s.Params["bind_address"])
		}
	}
}

func TestFindStrategyFragment_LegacyRefused(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "agent-config.yaml")
	if err := os.WriteFile(main, []byte("probes:\n  - name: cpu\n    type: cpu\nstorage:\n  - name: http\n    type: http\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := FindStrategyFragment(main, "http"); err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Errorf("legacy monolithic must be refused with guidance, got: %v", err)
	}
}

func TestSetStrategyScalar_NoFragment(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"k\"\n"), 0o600)
	os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o750)
	if err := SetStrategyScalar(main, "http", "port", "9090", "!!int"); err == nil || !strings.Contains(err.Error(), "no \"http\"") {
		t.Errorf("a missing http fragment must be reported, got: %v", err)
	}
}
