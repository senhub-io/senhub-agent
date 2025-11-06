package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestConfigMigrator_MigrateV1ToV2(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "config-migration-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create v1 config file (without 'type' field)
	configPath := filepath.Join(tempDir, "test-config.yaml")
	v1Config := `agent:
  key: "test-key-12345"
  mode: offline
  generated: false

auto_update:
  enabled: false
  url: "https://example.com/releases"

cache:
  retention_minutes: 5

storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
      endpoints: ["prtg", "web"]

probes:
  - name: cpu
    params:
      interval: 30
  - name: memory
    params:
      interval: 30
  - name: citrix
    params:
      base_url: "https://director.example.com"
      interval: 120
`

	if err := os.WriteFile(configPath, []byte(v1Config), 0600); err != nil {
		t.Fatalf("Failed to write v1 config: %v", err)
	}

	// Create migrator with minimal args
	testArgs := &cliArgs.ParsedArgs{
		DebugLogShipperUrl: "", // No log shipping in tests
	}
	baseLogger := logger.NewLogger(testArgs)
	migrator := NewConfigMigrator(configPath, baseLogger)

	// Run migration
	if err := migrator.MigrateIfNeeded(); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Check backup was created
	backupFiles, err := filepath.Glob(configPath + ".backup.*")
	if err != nil {
		t.Fatalf("Failed to glob backup files: %v", err)
	}
	if len(backupFiles) != 1 {
		t.Errorf("Expected 1 backup file, got %d", len(backupFiles))
	}

	// Read migrated config
	migratedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read migrated config: %v", err)
	}

	migratedContent := string(migratedData)

	// Verify migration header exists
	if !strings.Contains(migratedContent, "Configuration automatically migrated to version 2 format") {
		t.Error("Migration header not found in migrated config")
	}

	// Parse migrated YAML
	var config map[string]interface{}
	if err := yaml.Unmarshal(migratedData, &config); err != nil {
		t.Fatalf("Failed to parse migrated YAML: %v", err)
	}

	// Verify probes have 'type' field
	probesRaw, ok := config["probes"]
	if !ok {
		t.Fatal("No probes section in migrated config")
	}

	probes, ok := probesRaw.([]interface{})
	if !ok {
		t.Fatal("Probes section is not a list")
	}

	if len(probes) != 3 {
		t.Errorf("Expected 3 probes, got %d", len(probes))
	}

	// Check each probe has 'type' field
	for i, probeRaw := range probes {
		// yaml.v3 returns map[string]interface{} when unmarshaling into map[string]interface{}
		probe, ok := probeRaw.(map[string]interface{})
		if !ok {
			t.Errorf("Probe %d is not a map", i)
			continue
		}

		name, hasName := probe["name"]
		if !hasName {
			t.Errorf("Probe %d missing 'name' field", i)
		}

		probeType, hasType := probe["type"]
		if !hasType {
			t.Errorf("Probe %d missing 'type' field", i)
		}

		// Type should match name in default migration
		if name != probeType {
			t.Errorf("Probe %d: type (%v) doesn't match name (%v)", i, probeType, name)
		}

		t.Logf("Probe %d: name=%v, type=%v ✓", i, name, probeType)
	}

	// Test that running migration again doesn't create another backup
	initialBackupCount := len(backupFiles)
	if err := migrator.MigrateIfNeeded(); err != nil {
		t.Fatalf("Second migration check failed: %v", err)
	}

	backupFiles, err = filepath.Glob(configPath + ".backup.*")
	if err != nil {
		t.Fatalf("Failed to glob backup files after second check: %v", err)
	}

	if len(backupFiles) != initialBackupCount {
		t.Errorf("Second migration created unnecessary backup: expected %d files, got %d",
			initialBackupCount, len(backupFiles))
	}
}

func TestConfigMigrator_ValidateMigratedConfig(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "config-validation-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create v2 config file (with 'type' field)
	configPath := filepath.Join(tempDir, "test-config-v2.yaml")
	v2Config := `agent:
  key: "test-key-12345"
  mode: offline

probes:
  - name: cpu
    type: cpu
    params:
      interval: 30
  - name: Production Citrix
    type: citrix
    params:
      base_url: "https://director.example.com"
`

	if err := os.WriteFile(configPath, []byte(v2Config), 0600); err != nil {
		t.Fatalf("Failed to write v2 config: %v", err)
	}

	// Create migrator with minimal args
	testArgs := &cliArgs.ParsedArgs{
		DebugLogShipperUrl: "", // No log shipping in tests
	}
	baseLogger := logger.NewLogger(testArgs)
	migrator := NewConfigMigrator(configPath, baseLogger)

	// Validate configuration
	if err := migrator.ValidateMigratedConfig(); err != nil {
		t.Errorf("Valid v2 config failed validation: %v", err)
	}

	// Test invalid config (missing 'type')
	invalidConfigPath := filepath.Join(tempDir, "invalid-config.yaml")
	invalidConfig := `probes:
  - name: cpu
    params:
      interval: 30
`
	if err := os.WriteFile(invalidConfigPath, []byte(invalidConfig), 0600); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	invalidMigrator := NewConfigMigrator(invalidConfigPath, baseLogger)
	if err := invalidMigrator.ValidateMigratedConfig(); err == nil {
		t.Error("Invalid config should have failed validation")
	}
}

func TestConfigMigrator_NoMigrationNeeded(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "no-migration-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create v2 config (already has 'type')
	configPath := filepath.Join(tempDir, "test-config-v2.yaml")
	v2Config := `probes:
  - name: cpu
    type: cpu
    params:
      interval: 30
`

	if err := os.WriteFile(configPath, []byte(v2Config), 0600); err != nil {
		t.Fatalf("Failed to write v2 config: %v", err)
	}

	// Create migrator with minimal args
	testArgs := &cliArgs.ParsedArgs{
		DebugLogShipperUrl: "", // No log shipping in tests
	}
	baseLogger := logger.NewLogger(testArgs)
	migrator := NewConfigMigrator(configPath, baseLogger)

	// Migration should be skipped
	if err := migrator.MigrateIfNeeded(); err != nil {
		t.Fatalf("Migration check failed: %v", err)
	}

	// No backup should be created
	backupFiles, err := filepath.Glob(configPath + ".backup.*")
	if err != nil {
		t.Fatalf("Failed to glob backup files: %v", err)
	}

	if len(backupFiles) != 0 {
		t.Errorf("Backup created for v2 config (should skip migration): found %d files", len(backupFiles))
	}
}
