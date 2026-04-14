// Configuration management - offline config generation and validation
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

func generateOfflineConfiguration(args *cliArgs.ParsedArgs) error {
	// Import the configuration package
	appLogger := agentLogger.NewLogger(args)

	// Create local configuration instance
	localConfig := configuration.NewLocalConfiguration(args, appLogger)

	// Start the local configuration to trigger creation
	quitChannel := make(chan struct{})
	defer close(quitChannel)

	if err := localConfig.Start(quitChannel); err != nil {
		return fmt.Errorf("failed to create configuration: %w", err)
	}

	return nil
}

// cleanupFiles removes configuration files, logs, and certificates during uninstall
func cleanupFiles(args *cliArgs.ParsedArgs) {
	var filesToRemove []string
	var dirsToRemove []string

	// Configuration file - use absolute path to ensure correct file is found
	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		// Fallback to provided path if absolute path resolution fails
		configPath = args.ConfigPath
		if configPath == "" {
			configPath = "./agent-config.yaml"
		}
	}
	if _, statErr := os.Stat(configPath); statErr == nil {
		filesToRemove = append(filesToRemove, configPath)
	}

	// Certificate directory (use absolute path)
	currentDir, err := os.Getwd()
	if err == nil {
		certsDir := filepath.Join(currentDir, "certs")
		if _, err := os.Stat(certsDir); err == nil {
			dirsToRemove = append(dirsToRemove, certsDir)
		}
	}

	// Log files and directory
	logPaths := []string{
		"/Library/Logs/SenHub",          // macOS
		"/var/log/senhub-agent",         // Linux
		"C:\\ProgramData\\SenHub\\Logs", // Windows
		"./logs",                        // Local logs if any
	}

	for _, logPath := range logPaths {
		if _, err := os.Stat(logPath); err == nil {
			dirsToRemove = append(dirsToRemove, logPath)
		}
	}

	// Remove files
	for _, file := range filesToRemove {
		if err := os.Remove(file); err != nil {
			fmt.Printf("Warning: Could not remove %s: %v\n", file, err)
		} else {
			fmt.Printf("✅ Removed: %s\n", file)
		}
	}

	// Remove directories
	for _, dir := range dirsToRemove {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Printf("Warning: Could not remove directory %s: %v\n", dir, err)
		} else {
			fmt.Printf("✅ Removed directory: %s\n", dir)
		}
	}

	if len(filesToRemove) == 0 && len(dirsToRemove) == 0 {
		fmt.Println("✅ No additional files to clean up")
	} else {
		fmt.Printf("\n🧹 Cleanup completed - removed %d files and %d directories\n",
			len(filesToRemove), len(dirsToRemove))
	}
}

// showDebugModules displays all available debug modules

func validateConfigPath(configPath string) error {
	// Convert to absolute path for consistent validation
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Only allow .yaml and .yml extensions
	ext := strings.ToLower(filepath.Ext(absPath))
	if ext != ".yaml" && ext != ".yml" {
		return fmt.Errorf("config file must have .yaml or .yml extension, got: %s", ext)
	}

	// Ensure the path doesn't contain directory traversal attempts
	cleanPath := filepath.Clean(absPath)
	if cleanPath != absPath {
		return fmt.Errorf("path contains directory traversal attempts")
	}

	// Only allow config files in current directory or subdirectories (no parent directory access)
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Check if the file is within the working directory or its subdirectories
	relPath, err := filepath.Rel(workingDir, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("config file must be within the current working directory or its subdirectories")
	}

	return nil
}

// extractAgentKeyFromConfig attempts to extract agent key from local config file
func extractAgentKeyFromConfig(configPath string) (string, error) {
	// Validate the config path for security
	if err := validateConfigPath(configPath); err != nil {
		return "", fmt.Errorf("invalid config path: %w", err)
	}

	// This is a simplified version - in practice, we'd properly parse the YAML
	// #nosec G304 - path is validated by validateConfigPath function
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}

	// Simple string search for agent key (not ideal, but functional)
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "key:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(strings.Trim(parts[1], "\""))
				if key != "" {
					return key, nil
				}
			}
		}
	}

	return "", fmt.Errorf("agent key not found in config file")
}

// checkConfig validates a configuration file and reports issues
func checkConfig(configPath string) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		absPath = configPath
	}

	fmt.Printf("Checking configuration: %s\n\n", absPath)

	// Read and parse YAML
	content, err := os.ReadFile(configPath) // #nosec G304 - user-provided path for CLI tool
	if err != nil {
		fmt.Printf("  [ERROR] Cannot read file: %v\n", err)
		os.Exit(1)
	}

	var config configuration.LocalConfigurationData
	if err := yaml.Unmarshal(content, &config); err != nil {
		fmt.Printf("  [ERROR] Invalid YAML: %v\n", err)
		os.Exit(1)
	}

	errors := 0
	warnings := 0

	// Config version
	if config.ConfigVersion == 2 {
		fmt.Println("  [OK]   config_version: 2")
	} else if config.ConfigVersion == 0 {
		fmt.Println("  [ERROR] config_version missing (should be 2)")
		errors++
	} else {
		fmt.Printf("  [WARN] config_version: %d (expected 2)\n", config.ConfigVersion)
		warnings++
	}

	// Agent key
	if config.Agent.Key != "" {
		fmt.Printf("  [OK]   agent.key: %s\n", config.Agent.Key)
	} else {
		fmt.Println("  [ERROR] agent.key is missing")
		errors++
	}

	// Agent mode
	if config.Agent.Mode == "offline" || config.Agent.Mode == "online" {
		fmt.Printf("  [OK]   agent.mode: %s\n", config.Agent.Mode)
	} else if config.Agent.Mode == "" {
		fmt.Println("  [WARN] agent.mode not set (defaults to online)")
		warnings++
	} else {
		fmt.Printf("  [ERROR] agent.mode: invalid value %q (must be online or offline)\n", config.Agent.Mode)
		errors++
	}

	// License
	if config.Agent.License != "" {
		validator, validatorErr := license.GetDefaultValidator(7)
		if validatorErr != nil {
			fmt.Printf("  [WARN] Cannot initialize license validator: %v\n", validatorErr)
			warnings++
		} else {
			lic, licErr := validator.ValidateLicense(config.Agent.License)
			if licErr != nil {
				fmt.Printf("  [ERROR] agent.license: invalid (%v)\n", licErr)
				errors++
			} else {
				format := "JWT"
				if license.IsCompactLicense(config.Agent.License) {
					format = "compact"
				}
				fmt.Printf("  [OK]   agent.license: %s format, tier=%s, expires=%s\n",
					format, lic.Tier, lic.ExpiresAt.Format("2006-01-02"))

				if lic.IsExpired {
					fmt.Println("  [WARN] License is EXPIRED")
					warnings++
				}

				// Verify binding
				if config.Agent.Key != "" && !license.VerifyBinding(config.Agent.License, config.Agent.Key, lic) {
					fmt.Println("  [ERROR] License is not bound to this agent key")
					errors++
				} else if config.Agent.Key != "" {
					fmt.Println("  [OK]   License binding verified")
				}
			}
		}
	} else {
		fmt.Println("  [WARN] agent.license not set (free tier only)")
		warnings++
	}

	// Probes
	if len(config.Probes) == 0 {
		fmt.Println("  [WARN] No probes configured")
		warnings++
	} else {
		fmt.Printf("  [OK]   %d probe(s) configured\n", len(config.Probes))
		registeredProbes := probes.GetRegisteredProbeTypes()
		for _, p := range config.Probes {
			if p.Name == "" {
				fmt.Println("  [ERROR] Probe with empty name")
				errors++
				continue
			}
			if p.Type == "" {
				fmt.Printf("  [ERROR] Probe %q: type is missing\n", p.Name)
				errors++
				continue
			}
			if !registeredProbes[p.Type] {
				fmt.Printf("  [ERROR] Probe %q: unknown type %q\n", p.Name, p.Type)
				errors++
			} else {
				fmt.Printf("  [OK]   Probe %q (type: %s)\n", p.Name, p.Type)
			}
		}
	}

	// Storage
	if len(config.Storage) == 0 {
		fmt.Println("  [WARN] No storage strategies configured")
		warnings++
	} else {
		for _, s := range config.Storage {
			fmt.Printf("  [OK]   Storage: %s\n", s.Name)
		}
	}

	// Summary
	fmt.Println()
	if errors == 0 && warnings == 0 {
		fmt.Println("Configuration is valid.")
	} else if errors == 0 {
		fmt.Printf("Configuration is valid with %d warning(s).\n", warnings)
	} else {
		fmt.Printf("Configuration has %d error(s) and %d warning(s).\n", errors, warnings)
		os.Exit(1)
	}
}
