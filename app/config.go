// Configuration management - config generation and validation
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/lifecycle"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	"senhub-agent.go/internal/agent/services/license"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

// generateConfiguration runs at install time. In addition to
// triggering the LocalConfiguration loader (which creates the
// config-file parent directory and writes a default config), it
// eagerly creates the multi-file layout directories (probes.d,
// strategies.d) and the log directory so the first `agent run` finds
// a fully-formed install layout.
func generateConfiguration(args *cliArgs.ParsedArgs) error {
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

	// Install is a one-shot: the loader is started only to create and
	// seal the configuration, then stopped so its watcher goroutine does
	// not outlive the command.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := localConfig.Start(ctx); err != nil {
		return fmt.Errorf("failed to create configuration: %w", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), lifecycle.DefaultStopBudget)
	defer stopCancel()
	if err := localConfig.Shutdown(stopCtx); err != nil {
		appLogger.Warn().Err(err).Msg("Configuration loader did not stop cleanly after install")
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

	// The pre-0.5.4 second copy of the binary (#794). Uninstalling must not
	// leave behind an executable owned by a service user that is about to be
	// orphaned: nothing runs it any more, but it is still a writable binary
	// sitting in a state directory, which is exactly the shape this release
	// removed.
	if _, err := os.Stat(legacyManagedBinaryDir); err == nil {
		dirsToRemove = append(dirsToRemove, legacyManagedBinaryDir)
	}

	// Remove files
	for _, file := range filesToRemove {
		if err := os.Remove(file); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not remove %s: %v\n", file, err)
		} else {
			fmt.Printf("Removed: %s\n", file)
		}
	}

	// Remove directories
	for _, dir := range dirsToRemove {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not remove directory %s: %v\n", dir, err)
		} else {
			fmt.Printf("Removed directory: %s\n", dir)
		}
	}

	if len(filesToRemove) == 0 && len(dirsToRemove) == 0 {
		fmt.Println("No additional files to clean up")
	} else {
		fmt.Printf("\nCleanup completed - removed %d files and %d directories\n",
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

	// Accept the installed configuration, wherever the platform puts it,
	// plus anything under the working directory (a local bench, a config
	// staged next to the binary).
	//
	// Restricting this to the working directory alone made `status`
	// unusable on every normal install: the config lives in
	// /etc/senhub-agent or C:\ProgramData\SenHub, never under the
	// directory an operator happens to run the command from. The key
	// could not be read, so the command never reached the daemon and
	// silently printed its degraded local view instead.
	if withinInstalledConfigDir(absPath) {
		return nil
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	relPath, err := filepath.Rel(workingDir, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("config file must be the installed configuration or live under the current directory")
	}

	return nil
}

// withinInstalledConfigDir reports whether path sits in the directory
// this platform installs the agent configuration into.
func withinInstalledConfigDir(path string) bool {
	installed, err := cliArgs.GetAbsoluteConfigPath("")
	if err != nil {
		return false
	}
	dir := filepath.Dir(installed)
	rel, err := filepath.Rel(dir, path)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// extractAgentKeyFromConfig attempts to extract agent key from local config file
func extractAgentKeyFromConfig(configPath string) (string, error) {
	// Validate the config path for security
	if err := validateConfigPath(configPath); err != nil {
		return "", fmt.Errorf("invalid config path: %w", err)
	}

	// Resolve through the real loader first: since 0.5.x an install seals
	// the key, so the file holds "${secret:agent.key}" and a text search
	// hands that literal to the API. Auth then fails and `status` silently
	// falls back to its degraded local view — on every modern host.
	if cfg, err := configuration.LoadForShow(configPath, configuration.ShowResolved, nil); err == nil {
		if key := strings.TrimSpace(cfg.Agent.Key); key != "" && !strings.Contains(key, "${") {
			return key, nil
		}
	}

	// Fallback: a plain key in a file the loader could not read (a partial
	// or hand-written config still deserves a working status).
	// #nosec G304 - path is validated by validateConfigPath function
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "key:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(strings.Trim(parts[1], "\""))
				// An unresolved reference is not a key: handing it to the
				// API fails authentication and sends the caller down the
				// degraded path with no clue why.
				if key != "" && !strings.Contains(key, "${") {
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
		var parseErr *configuration.ParseError
		if errors.As(err, &parseErr) {
			// Multi-file layouts fail on a probes.d/ or strategies.d/
			// fragment, not on the top-level file we already read.
			// Show the file the decoder actually choked on, and hand
			// the unwrapped decoder error over — the "yaml: line N:"
			// prefix it carries is what locates the offending line.
			src := content
			if parseErr.Path != configPath {
				if raw, readErr := os.ReadFile(parseErr.Path); readErr == nil { // #nosec G304 - path came from the loader walking the configured *.d/ directories
					src = raw
				}
			}
			showYAMLErrorContext(string(src), parseErr.Err)
		}
		os.Exit(1)
	}

	errorCount := 0
	warnings := 0

	// Config version. Validate against the agent's supported range
	// (MinimumConfigVersion..CurrentConfigVersion) rather than a
	// hardcoded literal, so `config check` tracks the source of truth
	// in config_version.go. A missing field defaults to version 1
	// (legacy) at load time, mirroring the loader.
	switch {
	case config.ConfigVersion == 0:
		fmt.Printf("  [ERROR] config_version missing (expected %d)\n", configuration.CurrentConfigVersion)
		errorCount++
	case configuration.ValidateConfigVersion(config.ConfigVersion) != nil:
		fmt.Printf("  [ERROR] config_version: %d (%v)\n",
			config.ConfigVersion, configuration.ValidateConfigVersion(config.ConfigVersion))
		errorCount++
	case config.ConfigVersion < configuration.CurrentConfigVersion:
		fmt.Printf("  [OK]   config_version: %d (agent supports up to %d; will migrate on next write)\n",
			config.ConfigVersion, configuration.CurrentConfigVersion)
	default:
		fmt.Printf("  [OK]   config_version: %d\n", config.ConfigVersion)
	}

	// Agent key
	if config.Agent.Key != "" {
		fmt.Printf("  [OK]   agent.key: %s\n", config.Agent.Key)
	} else {
		fmt.Println("  [ERROR] agent.key is missing")
		errorCount++
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
				errorCount++
			} else {
				fmt.Printf("  [OK]   agent.license: tier=%s, expires=%s\n",
					lic.Tier, lic.ExpiresAt.Format("2006-01-02"))

				if lic.IsExpired {
					fmt.Println("  [WARN] License is EXPIRED")
					warnings++
				}

				// Verify binding
				if config.Agent.Key != "" && !license.VerifyBinding(config.Agent.License, config.Agent.Key, lic) {
					fmt.Println("  [ERROR] License is not bound to this agent key")
					errorCount++
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
		disabled := 0
		for _, p := range config.Probes {
			if !p.IsEnabled() {
				disabled++
			}
		}
		if disabled > 0 {
			// Stated up front rather than buried per probe: "n configured" and
			// "n collecting" being different numbers is the first thing an
			// operator needs to know when data is missing.
			fmt.Printf("  [OK]   %d probe(s) configured, %d disabled\n", len(config.Probes), disabled)
		} else {
			fmt.Printf("  [OK]   %d probe(s) configured\n", len(config.Probes))
		}
		registeredProbes := probes.GetRegisteredProbeTypes()
		for _, p := range config.Probes {
			if p.Name == "" {
				fmt.Println("  [ERROR] Probe with empty name")
				errorCount++
				continue
			}
			if p.Type == "" {
				fmt.Printf("  [ERROR] Probe %q: type is missing\n", p.Name)
				errorCount++
				continue
			}
			if !registeredProbes[p.Type] {
				fmt.Printf("  [ERROR] Probe %q: unknown type %q\n", p.Name, p.Type)
				errorCount++
				continue
			}
			// An output that cannot consume logs would silently swallow
			// this probe's records: they would be routed to it, and it
			// would never read the log rail. Refuse the value instead of
			// letting the operator discover it as missing data (#836).
			badRouting := false
			for _, target := range p.LogStrategies {
				if !configuration.IsLogCapableStrategy(target) {
					fmt.Printf("  [ERROR] Probe %q: log_strategies names %q, which cannot receive logs (accepted: %s)\n",
						p.Name, target, strings.Join(configuration.LogCapableStrategies, ", "))
					errorCount++
					badRouting = true
				}
			}
			if badRouting {
				// Don't follow an ERROR with an OK line describing the
				// routing that was just rejected — the operator would
				// have to read both to know which one holds.
				continue
			}

			if !p.IsEnabled() {
				fmt.Printf("  [OFF]  Probe %q (type: %s) - disabled, will not collect\n", p.Name, p.Type)
			} else if len(p.LogStrategies) > 0 {
				fmt.Printf("  [OK]   Probe %q (type: %s), logs routed to %s\n",
					p.Name, p.Type, strings.Join(p.LogStrategies, ", "))
			} else {
				fmt.Printf("  [OK]   Probe %q (type: %s)\n", p.Name, p.Type)
			}

			// Validate required params per probe type
			e, w := validateProbeParams(p.Name, p.Type, p.Params)
			errorCount += e
			warnings += w
		}
	}

	// Storage
	if len(config.Storage) == 0 {
		fmt.Println("  [WARN] No storage strategies configured")
		warnings++
	} else {
		validStrategies := map[string]bool{"http": true, "prtg": true, "senhub": true, "event": true, "otlp": true}
		for _, s := range config.Storage {
			if !validStrategies[s.Name] {
				fmt.Printf("  [WARN] Storage %q: unknown strategy\n", s.Name)
				warnings++
				continue
			}
			if s.Name == "otlp" {
				if verr := otlp.ValidateEntitiesRedactAttributes(s.Params); verr != nil {
					fmt.Printf("  [ERROR] Storage %q: %v\n", s.Name, verr)
					errorCount++
					continue
				}
			}
			fmt.Printf("  [OK]   Storage: %s\n", s.Name)
		}
	}

	// Binary writability. What is correct differs per platform: on Linux the
	// daemon must NOT be able to write its own executable (#794), everywhere
	// else the in-process updater needs to (#377). checkAutoUpdateWritability
	// reports whichever is wrong for the platform it runs on.
	//
	// Checked whenever the agent is configured, not only when auto_update is
	// enabled: on Linux a writable binary is a security finding on its own, and
	// turning auto-update off does not make it safe.
	// The registry URL is a BASE the agent appends to. A value carrying
	// a path the agent adds itself doubles it, and updates then fail
	// silently — the host stays on its installed version with
	// `enabled: true` still in the file. Say so at check time rather
	// than letting it be discovered months later (#747, #840).
	if config.AutoUpdate != nil {
		if p := configuration.CheckRegistryURL(config.AutoUpdate.URL); p != nil {
			if p.Suggestion != "" {
				fmt.Printf("  [WARN] auto_update.url %s\n", p.Reason)
				fmt.Printf("         configured: %s\n", config.AutoUpdate.URL)
				fmt.Printf("         write instead: %s\n", p.Suggestion)
				warnings++
			} else {
				fmt.Printf("  [ERROR] auto_update.url %s\n", p.Reason)
				fmt.Printf("          configured: %s\n", config.AutoUpdate.URL)
				errorCount++
			}
		}
	}

	if warn := checkAutoUpdateWritability(); warn != "" {
		fmt.Printf("  [WARN] %s\n", warn)
		warnings++
	} else if config.AutoUpdate != nil && config.AutoUpdate.Enabled {
		if runtime.GOOS == "linux" {
			fmt.Println("  [OK]   auto_update.enabled: new versions are reported; install them with 'sudo senhub-agent update'")
		} else {
			fmt.Println("  [OK]   auto_update.enabled: binary is replaceable in place")
		}
	}

	// Summary
	fmt.Println()
	if errorCount == 0 && warnings == 0 {
		fmt.Println("Configuration is valid.")
	} else if errorCount == 0 {
		fmt.Printf("Configuration is valid with %d warning(s).\n", warnings)
	} else {
		fmt.Printf("Configuration has %d error(s) and %d warning(s).\n", errorCount, warnings)
		os.Exit(1)
	}
}

// showConfig prints the merged configuration as YAML for diffability
// and audit. Modes:
//
//	--resolved (default) — ${env:..} / ${file:..} references resolved
//	                       against the current environment / FS.
//	                       This is what the agent boots with.
//	--raw                — references preserved as written, useful
//	                       for reviewing the loaded layout before
//	                       comparing against the resolved output.
//	--redact             — the DEFAULT: resolved, but values that came
//	                       from ${file:..} OR sit under a YAML key whose
//	                       name matches (?i)(key|token|password|secret)
//	                       are masked with "***". Safe for tickets.
//	--resolved           — resolved WITHOUT redaction: secrets in
//	                       cleartext. Requires the explicit flag —
//	                       operators paste config show into tickets,
//	                       so the safe behavior is the default (#279).
//
// Output: YAML, with map keys sorted alphabetically (yaml.v3 + a
// post-pass over the marshaled node tree) so two runs produce
// byte-identical output and dashboards/diffs stay stable.
//
// Errors abort with exit 1 and a single human-readable line on
// stderr — the goal is "fits in a CI log".
func showConfig(args []string) {
	mode := configuration.ShowRedact
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
				fmt.Fprintf(os.Stderr, "Error: config show: unknown flag %q\n", a)
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
		fatalf("config show: %v", err)
	}

	out, err := configuration.MarshalSortedYAML(&data)
	if err != nil {
		fatalf("config show: marshaling output: %v", err)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fatalf("config show: write: %v", err)
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
