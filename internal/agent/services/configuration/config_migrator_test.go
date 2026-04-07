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

func TestConfigMigrator_MigrateParamRenames(t *testing.T) {
	testArgs := &cliArgs.ParsedArgs{
		DebugLogShipperUrl: "",
	}
	baseLogger := logger.NewLogger(testArgs)

	// We need a valid config path for the migrator, even though we test the method directly
	tempDir, err := os.MkdirTemp("", "param-rename-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	configPath := filepath.Join(tempDir, "dummy.yaml")
	os.WriteFile(configPath, []byte(""), 0600)
	migrator := NewConfigMigrator(configPath, baseLogger)

	t.Run("citrix probe with base_url should rename to director_url", func(t *testing.T) {
		probesList := []interface{}{
			map[string]interface{}{
				"name": "Production Citrix",
				"type": "citrix",
				"params": map[string]interface{}{
					"base_url": "https://director.example.com",
					"interval": 120,
				},
			},
		}

		changed := migrator.migrateParamRenames(probesList)
		if !changed {
			t.Error("Expected migrateParamRenames to return true for citrix base_url rename")
		}

		probe := probesList[0].(map[string]interface{})
		params := probe["params"].(map[string]interface{})

		if _, hasOld := params["base_url"]; hasOld {
			t.Error("base_url should have been removed after rename")
		}
		if val, hasNew := params["director_url"]; !hasNew {
			t.Error("director_url should have been created after rename")
		} else if val != "https://director.example.com" {
			t.Errorf("director_url value mismatch: got %v, want https://director.example.com", val)
		}
		// interval should be untouched
		if params["interval"] != 120 {
			t.Errorf("interval should be unchanged, got %v", params["interval"])
		}
	})

	t.Run("citrix probe with director_url should not change", func(t *testing.T) {
		probesList := []interface{}{
			map[string]interface{}{
				"name": "Production Citrix",
				"type": "citrix",
				"params": map[string]interface{}{
					"director_url": "https://director.example.com",
					"interval":     120,
				},
			},
		}

		changed := migrator.migrateParamRenames(probesList)
		if changed {
			t.Error("Expected migrateParamRenames to return false when director_url already exists")
		}

		params := probesList[0].(map[string]interface{})["params"].(map[string]interface{})
		if params["director_url"] != "https://director.example.com" {
			t.Errorf("director_url should be unchanged, got %v", params["director_url"])
		}
	})

	t.Run("citrix probe with both base_url and director_url should not change", func(t *testing.T) {
		probesList := []interface{}{
			map[string]interface{}{
				"name": "Production Citrix",
				"type": "citrix",
				"params": map[string]interface{}{
					"base_url":     "https://old-director.example.com",
					"director_url": "https://new-director.example.com",
				},
			},
		}

		changed := migrator.migrateParamRenames(probesList)
		if changed {
			t.Error("Expected migrateParamRenames to return false when both old and new params exist")
		}

		params := probesList[0].(map[string]interface{})["params"].(map[string]interface{})
		// Both should remain untouched
		if params["base_url"] != "https://old-director.example.com" {
			t.Errorf("base_url should be unchanged, got %v", params["base_url"])
		}
		if params["director_url"] != "https://new-director.example.com" {
			t.Errorf("director_url should be unchanged, got %v", params["director_url"])
		}
	})

	t.Run("non-citrix probe with base_url should not change", func(t *testing.T) {
		probesList := []interface{}{
			map[string]interface{}{
				"name": "My Redfish Server",
				"type": "redfish",
				"params": map[string]interface{}{
					"base_url": "https://bmc.example.com",
					"interval": 60,
				},
			},
		}

		changed := migrator.migrateParamRenames(probesList)
		if changed {
			t.Error("Expected migrateParamRenames to return false for non-citrix probe")
		}

		params := probesList[0].(map[string]interface{})["params"].(map[string]interface{})
		if _, has := params["base_url"]; !has {
			t.Error("base_url should remain for non-citrix probe")
		}
		if _, has := params["director_url"]; has {
			t.Error("director_url should not be added to non-citrix probe")
		}
	})

	t.Run("mixed probes only renames citrix base_url", func(t *testing.T) {
		probesList := []interface{}{
			map[string]interface{}{
				"name": "CPU Monitor",
				"type": "cpu",
				"params": map[string]interface{}{
					"interval": 30,
				},
			},
			map[string]interface{}{
				"name": "Production Citrix",
				"type": "citrix",
				"params": map[string]interface{}{
					"base_url": "https://director.example.com",
				},
			},
			map[string]interface{}{
				"name": "BMC Server",
				"type": "redfish",
				"params": map[string]interface{}{
					"base_url": "https://bmc.example.com",
				},
			},
		}

		changed := migrator.migrateParamRenames(probesList)
		if !changed {
			t.Error("Expected migrateParamRenames to return true (citrix base_url was renamed)")
		}

		// Citrix probe should have director_url
		citrixParams := probesList[1].(map[string]interface{})["params"].(map[string]interface{})
		if _, has := citrixParams["base_url"]; has {
			t.Error("citrix base_url should have been removed")
		}
		if citrixParams["director_url"] != "https://director.example.com" {
			t.Errorf("citrix director_url mismatch: got %v", citrixParams["director_url"])
		}

		// Redfish probe should still have base_url
		redfishParams := probesList[2].(map[string]interface{})["params"].(map[string]interface{})
		if redfishParams["base_url"] != "https://bmc.example.com" {
			t.Errorf("redfish base_url should be unchanged, got %v", redfishParams["base_url"])
		}
	})
}
