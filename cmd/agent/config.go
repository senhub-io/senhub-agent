// Configuration management - offline config generation and validation
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

// generateOfflineConfiguration runs at install time. In addition to
// triggering the LocalConfiguration loader (which creates the
// config-file parent directory and writes a default config), it
// eagerly creates the multi-file layout directories (probes.d,
// strategies.d) and the log directory so the first `agent run` finds
// a fully-formed install layout.
func generateOfflineConfiguration(args *cliArgs.ParsedArgs) error {
	appLogger := agentLogger.NewLogger(args)

	// Resolve absolute config path so we know where to create the
	// sibling probes.d / strategies.d directories.
	absConfigPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err == nil {
		configDir := filepath.Dir(absConfigPath)
		for _, sub := range []string{"probes.d", "strategies.d"} {
			d := filepath.Join(configDir, sub)
			if mkErr := os.MkdirAll(d, 0750); mkErr != nil {
				appLogger.Warn().Err(mkErr).Str("dir", d).Msg("Failed to pre-create install directory")
			}
		}
	}

	// Eager-create the log directory so it doesn't have to wait for
	// the first log write. Soft-fail (warn only) because the directory
	// is also lazy-created by the logger on first write.
	logDir := agentLogger.LogBaseDir()
	if mkErr := os.MkdirAll(logDir, 0750); mkErr != nil {
		appLogger.Warn().Err(mkErr).Str("dir", logDir).Msg("Failed to pre-create log directory at install time")
	}

	localConfig := configuration.NewLocalConfiguration(args, appLogger)

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

	// Log files and directory. Paths must match exactly what
	// logger.LogBaseDir() returns; in 0.2.0+ Linux moved from
	// /var/log/senhub to /var/log/senhub-agent, and the Windows
	// casing was lowercased (logs, not Logs).
	logPaths := []string{
		"/Library/Logs/SenHub",          // macOS
		"/var/log/senhub-agent",         // Linux (0.2.0+)
		"/var/log/senhub",               // Linux (pre-0.2.0) — cleanup leftover
		"C:\\ProgramData\\SenHub\\logs", // Windows (0.2.0+ casing)
		"C:\\ProgramData\\SenHub\\Logs", // Windows (pre-0.2.0 casing) — cleanup leftover
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

// checkConfig validates a configuration file and reports issues.
//
// In 0.2.x+ the agent supports two layouts (monolithic agent-config.yaml
// or multi-file agent.yaml + probes.d/ + strategies.d/) — they are
// auto-detected by configuration.LoadFromDisk. checkConfig now uses
// LoadFromDisk so the result of `agent config check` matches what the
// running agent actually sees: probes declared in probes.d/ fragments
// no longer fall through as "no probes configured" warnings.
//
// Errors that prevent loading (file missing, malformed YAML, broken
// substitution) abort with exit 1 + a context dump for YAML parse
// errors. Validation errors are collected, reported, and reflected in
// the final non-zero exit code.
func checkConfig(configPath string) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		absPath = configPath
	}

	fmt.Printf("Checking configuration: %s\n\n", absPath)

	// Read raw bytes once so YAML-syntax errors can still print a
	// useful "near this line" context (LoadFromDisk only returns a
	// wrapped error).
	content, err := os.ReadFile(configPath) // #nosec G304 - user-provided path for CLI tool
	if err != nil {
		fmt.Printf("  [ERROR] Cannot read file: %v\n", err)
		os.Exit(1)
	}

	// Build a minimal logger for the loader so the WARN events
	// (legacy monolithic detection, duplicate strategy override) reach
	// the operator. WarnLevel keeps the noise low.
	zlog := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	base := (*agentLogger.Logger)(&zlog)
	loaderLog := agentLogger.NewModuleLogger(base, "configuration.check")

	config, err := configuration.LoadFromDisk(configPath, loaderLog)
	if err != nil {
		// LoadFromDisk returns wrapped errors for: open failures,
		// YAML parse failures, substitution failures. We've already
		// confirmed the file is readable; the most operator-useful
		// case is a YAML parse error — show the near-context block.
		fmt.Println("  [ERROR] Configuration load failed")
		fmt.Printf("           %v\n", err)
		fmt.Println()
		if strings.Contains(err.Error(), "yaml") {
			showYAMLErrorContext(string(content), err)
		}
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

	// Agent mode. In 0.2.0+ offline is the only supported mode;
	// "online" is accepted with a deprecation warning so that operators
	// upgrading from a pre-0.2.0 install with `mode: online` still
	// pass the config-check (the agent ignores the field anyway).
	switch config.Agent.Mode {
	case "offline", "":
		fmt.Printf("  [OK]   agent.mode: offline\n")
	case "online":
		fmt.Println("  [WARN] agent.mode: online (no longer supported in 0.2.0+ — agent ignores this value and runs offline). Update the config to mode: offline.")
		warnings++
	default:
		fmt.Printf("  [ERROR] agent.mode: invalid value %q (must be omitted or set to offline)\n", config.Agent.Mode)
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
				continue
			}
			fmt.Printf("  [OK]   Probe %q (type: %s)\n", p.Name, p.Type)

			// Validate required params per probe type
			e, w := validateProbeParams(p.Name, p.Type, p.Params)
			errors += e
			warnings += w
		}
	}

	// Storage
	if len(config.Storage) == 0 {
		fmt.Println("  [WARN] No storage strategies configured")
		warnings++
	} else {
		validStrategies := map[string]bool{"http": true, "prtg": true, "senhub": true, "event": true}
		for _, s := range config.Storage {
			if !validStrategies[s.Name] {
				fmt.Printf("  [WARN] Storage %q: unknown strategy\n", s.Name)
				warnings++
			} else {
				fmt.Printf("  [OK]   Storage: %s\n", s.Name)
			}
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

// showConfig prints the merged configuration as YAML for diffability
// and audit. Modes:
//
//   --resolved (default) — ${env:..} / ${file:..} references resolved
//                          against the current environment / FS.
//                          This is what the agent boots with.
//   --raw                — references preserved as written, useful
//                          for reviewing the loaded layout before
//                          comparing against the resolved output.
//   --redact             — resolved, but values that came from
//                          ${file:..} OR sit under a YAML key whose
//                          name matches (?i)(key|token|password|secret)
//                          are masked with "***". Safe for tickets.
//
// Output: YAML, with map keys sorted alphabetically (yaml.v3 + a
// post-pass over the marshaled node tree) so two runs produce
// byte-identical output and dashboards/diffs stay stable.
//
// Errors abort with exit 1 and a single human-readable line on
// stderr — the goal is "fits in a CI log".
func showConfig(args []string) {
	mode := configuration.ShowResolved
	// Empty string means "use the OS-canonical default" — resolved
	// below via GetAbsoluteConfigPath. An explicit positional path
	// argument overrides it.
	configPath := ""

	for _, a := range args {
		switch a {
		case "--raw":
			mode = configuration.ShowRaw
		case "--resolved":
			mode = configuration.ShowResolved
		case "--redact":
			mode = configuration.ShowRedact
		case "-h", "--help":
			fmt.Println("Usage: agent config show [--raw|--resolved|--redact] [path]")
			return
		default:
			if strings.HasPrefix(a, "--") {
				fmt.Fprintf(os.Stderr, "config show: unknown flag %q\n", a)
				os.Exit(2)
			}
			configPath = a
		}
	}

	// Resolve to the OS-canonical absolute path when no explicit path
	// was given, mirroring what `agent run` / `agent start` use. The
	// pre-0.2.0 default was the working-directory-relative
	// ./agent-config.yaml, which diverged from where the agent
	// actually reads its config.
	if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
		configPath = resolved
	} else if absPath, absErr := filepath.Abs(configPath); absErr == nil {
		configPath = absPath
	}

	// We need a logger so the loader can WARN about legacy detection
	// and duplicate strategies. Build a minimal one writing to stderr
	// at WARN level; --verbose flips it to debug if the operator wants
	// to see the loader's chatter.
	zlog := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	base := (*agentLogger.Logger)(&zlog)
	log := agentLogger.NewModuleLogger(base, "configuration.show")

	data, err := configuration.LoadForShow(configPath, mode, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config show: %v\n", err)
		os.Exit(1)
	}

	out, err := configuration.MarshalSortedYAML(&data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config show: marshaling output: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintf(os.Stderr, "config show: write: %v\n", err)
		os.Exit(1)
	}
}

// showYAMLErrorContext shows the problematic line from the YAML file
func showYAMLErrorContext(content string, yamlErr error) {
	errMsg := yamlErr.Error()

	// Extract line number from "yaml: line N: ..."
	lineNum := 0
	if _, err := fmt.Sscanf(errMsg, "yaml: line %d:", &lineNum); err != nil || lineNum == 0 {
		fmt.Printf("  %s\n", errMsg)
		return
	}

	lines := strings.Split(content, "\n")
	fmt.Printf("  Error at line %d: %s\n\n", lineNum, errMsg[strings.Index(errMsg, ":")+2:])

	// Show context: 2 lines before, the error line, 2 lines after
	start := lineNum - 3
	if start < 0 {
		start = 0
	}
	end := lineNum + 2
	if end > len(lines) {
		end = len(lines)
	}

	for i := start; i < end; i++ {
		marker := "  "
		if i == lineNum-1 {
			marker = ">>"
		}
		fmt.Printf("  %s %3d | %s\n", marker, i+1, lines[i])
	}
	fmt.Println()

	// Common hints
	if strings.Contains(errMsg, "could not find expected ':'") {
		fmt.Println("  Hint: Check for missing space after ':' (e.g., 'key:value' should be 'key: value')")
	} else if strings.Contains(errMsg, "did not find expected") {
		fmt.Println("  Hint: Check indentation — YAML uses spaces, not tabs")
	}
}

// validateProbeParams checks required parameters for each probe type
func validateProbeParams(name, probeType string, params map[string]interface{}) (errors, warnings int) {
	// Citrix has two accepted formats: nested director block (0.1.87+) or flat director_url/base_url.
	// Validate manually instead of using a flat required-list.
	if probeType == "citrix" {
		return validateCitrixParams(name, params)
	}

	// Required params per probe type (flat format)
	requiredParams := map[string][]string{
		"veeam":        {"endpoint", "username", "password"},
		"netscaler":    {"base_url", "username", "password"},
		"redfish":      {"endpoint", "username", "password"},
		"ping_webapp":  {"url"},
		"load_webapp":  {"url"},
		"ping_gateway": {"destination"},
		"syslog":       {"listen_address"},
	}

	required, hasRequired := requiredParams[probeType]
	if !hasRequired {
		return 0, 0
	}

	for _, param := range required {
		val, exists := params[param]
		if !exists || val == nil || val == "" {
			fmt.Printf("         [ERROR] Probe %q: missing required param %q\n", name, param)
			errors++
		}
	}

	// Check for common misconfigurations
	if probeType == "veeam" {
		if interval, ok := params["interval"]; ok {
			if intVal, ok := interval.(int); ok && intVal < 60 {
				fmt.Printf("         [WARN] Probe %q: interval %ds is very short (recommended: 300s)\n", name, intVal)
				warnings++
			}
		}
	}

	return errors, warnings
}

// asStringMap coerces a value that may be map[string]interface{} or map[interface{}]interface{}
// (yaml.v2 decodes nested maps with interface keys) into map[string]interface{}.
func asStringMap(v interface{}) (map[string]interface{}, bool) {
	if m, ok := v.(map[string]interface{}); ok {
		return m, true
	}
	if mi, ok := v.(map[interface{}]interface{}); ok {
		out := make(map[string]interface{}, len(mi))
		for k, val := range mi {
			if ks, ok := k.(string); ok {
				out[ks] = val
			}
		}
		return out, true
	}
	return nil, false
}

// validateCitrixParams validates Citrix probe config (accepts nested director block or flat director_url/base_url).
func validateCitrixParams(name string, params map[string]interface{}) (errors, warnings int) {
	// New format: director: { url, auth: { username, password } }
	if director, ok := asStringMap(params["director"]); ok {
		if url, _ := director["url"].(string); url == "" {
			fmt.Printf("         [ERROR] Probe %q: missing required param %q\n", name, "director.url")
			errors++
		}
		auth, authOK := asStringMap(director["auth"])
		if !authOK {
			fmt.Printf("         [ERROR] Probe %q: missing required param %q\n", name, "director.auth")
			errors++
		} else {
			if u, _ := auth["username"].(string); u == "" {
				fmt.Printf("         [ERROR] Probe %q: missing required param %q\n", name, "director.auth.username")
				errors++
			}
			if p, _ := auth["password"].(string); p == "" {
				fmt.Printf("         [ERROR] Probe %q: missing required param %q\n", name, "director.auth.password")
				errors++
			}
		}
		return errors, warnings
	}

	// Legacy format: director_url (or base_url) + auth: { username, password }
	url, _ := params["director_url"].(string)
	if url == "" {
		url, _ = params["base_url"].(string)
	}
	if url == "" {
		fmt.Printf("         [ERROR] Probe %q: missing required param %q (or %q, or nested %q)\n", name, "director_url", "base_url", "director.url")
		errors++
	}
	auth, authOK := asStringMap(params["auth"])
	if !authOK {
		fmt.Printf("         [ERROR] Probe %q: missing required param %q (or nested %q)\n", name, "auth", "director.auth")
		errors++
	} else {
		if u, _ := auth["username"].(string); u == "" {
			fmt.Printf("         [ERROR] Probe %q: missing required param %q\n", name, "auth.username")
			errors++
		}
		if p, _ := auth["password"].(string); p == "" {
			fmt.Printf("         [ERROR] Probe %q: missing required param %q\n", name, "auth.password")
			errors++
		}
	}
	return errors, warnings
}
