// Configuration management - offline config generation and validation
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
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
