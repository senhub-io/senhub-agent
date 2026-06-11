package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kardianos/service"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/debugshipper"
)

// Expose log levels
const (
	DebugLevel = zerolog.DebugLevel
	InfoLevel  = zerolog.InfoLevel
	WarnLevel  = zerolog.WarnLevel
	ErrorLevel = zerolog.ErrorLevel
	FatalLevel = zerolog.FatalLevel
	PanicLevel = zerolog.PanicLevel
)

// Logger is an alias type for zerolog.Logger
type Logger = zerolog.Logger

// LoggerConfig holds the configuration for the logger
type LoggerConfig struct {
	logShipper io.Writer
}

// ModuleLogConfig allows setting specific log levels for different components
type ModuleLogConfig struct {
	Module string `json:"module" yaml:"module"`
	Level  string `json:"level" yaml:"level"`
}

// getLogPath determines the appropriate log file location based on the
// operating system. OS-canonical paths in 0.2.0+:
//   - Linux:   /var/log/senhub-agent/senhubagent.log
//   - Windows: %ProgramData%\SenHub\logs\senhubagent.log
//   - macOS:   /Library/Logs/SenHub/senhubagent.log
//
// Pre-0.2.0 the Linux path was /var/log/senhub (without "-agent" suffix).
// LogBaseDir() exposes the canonical directory so install / uninstall
// can share the same constant.
func getLogPath() string {
	basePath := LogBaseDir()

	// Attempt to create the log directory and test write permissions
	logPath := filepath.Join(basePath, "senhubagent.log")
	if err := os.MkdirAll(basePath, 0750); err != nil {
		log.Printf("Unable to create log directory %s: %v", basePath, err)
		// Fall back to executable directory
		exePath, _ := os.Executable()
		basePath = filepath.Dir(exePath)
		logPath = filepath.Join(basePath, "senhubagent.log")
	} else {
		// Test write permissions by trying to create a test file
		testFile := filepath.Join(basePath, ".write_test")
		if file, err := os.Create(filepath.Clean(testFile)); err != nil { // #nosec G304 - testFile is constructed from safe basePath
			log.Printf("No write permissions for log directory %s: %v", basePath, err)
			log.Printf("Falling back to local directory for logs")
			// Fall back to executable directory
			exePath, _ := os.Executable()
			basePath = filepath.Dir(exePath)
			logPath = filepath.Join(basePath, "senhubagent.log")
		} else {
			_ = file.Close()
			_ = os.Remove(testFile)
		}
	}

	// Only print log file path when not running status command or tests
	if len(os.Args) < 2 || (os.Args[1] != "status" && !isInTestMode()) {
		log.Printf("Using log file: %s", logPath)
	}
	return logPath
}

// isInTestMode detects if the current execution is running in test mode
func isInTestMode() bool {
	for _, arg := range os.Args {
		if strings.Contains(arg, "test") || strings.HasSuffix(arg, ".test") {
			return true
		}
	}
	return false
}

// NewLogger creates a new logger instance based on the provided arguments.
// It switches between development and production configurations based on the environment.
// setupDebugLogShipper creates a new DebugLogShipper based on CLI arguments
func setupDebugLogShipper(args *cliArgs.ParsedArgs) (io.Writer, error) {
	if args.DebugLogShipperUrl == "" {
		return nil, nil // No debug log shipping configured
	}

	// Create config for debug log shipper
	config := debugshipper.DefaultConfig()
	config.Endpoint = args.DebugLogShipperUrl

	if args.DebugLogShipperBuffer > 0 {
		config.BufferSize = args.DebugLogShipperBuffer
	}

	// Set tags if provided
	if len(args.DebugLogShipperTags) > 0 {
		config.Tags = args.DebugLogShipperTags

		// For VictoriaLogs, add agent_name, host_name, and env tags if not present
		if _, ok := config.Tags["agent_name"]; !ok {
			config.Tags["agent_name"] = "senhub-agent"
		}

		if _, ok := config.Tags["host_name"]; !ok {
			hostname, err := os.Hostname()
			if err == nil && hostname != "" {
				config.Tags["host_name"] = hostname
			}
		}

		if _, ok := config.Tags["env"]; !ok {
			config.Tags["env"] = args.Env
		}
	}

	log.Printf("Initializing debug log shipper to %s", args.DebugLogShipperUrl)

	// Initialize the debug log shipper
	shipper, err := debugshipper.NewDebugLogShipper(config)
	if err != nil {
		log.Printf("Failed to initialize debug log shipper: %v", err)
		return nil, err
	}

	return shipper, nil
}

func NewLogger(args *cliArgs.ParsedArgs) *Logger {
	var logger *Logger

	// Create debug log shipper if configured
	shipper, err := setupDebugLogShipper(args)
	if err != nil {
		log.Printf("Warning: Failed to create debug log shipper: %v", err)
	}

	// Create logger configuration
	config := &LoggerConfig{
		logShipper: shipper,
	}

	switch args.Env {
	case "development":
		logger = buildDevelopmentLogger(args, config)
	default:
		logger = buildProductionLogger(args, config)
	}

	// Reset the selective-debug state for this logger construction.
	mutateLevelState(func(st *levelState) {
		st.selective = false
		st.debugModules = map[string]bool{}
	})

	// Configure debug logging based on CLI flags
	if args.Verbose {
		if len(args.DebugModules) > 0 {
			// Selective debug mode: --verbose with --debug-modules
			// Only specified modules will output debug logs
			// All modules continue to output Info/Warn/Error

			// Keep global level at INFO for non-module logs
			zerolog.SetGlobalLevel(zerolog.InfoLevel)

			// Enable debug only for specified modules
			mutateLevelState(func(st *levelState) {
				st.selective = true
				for _, module := range args.DebugModules {
					st.levels[module] = zerolog.DebugLevel
					st.debugModules[module] = true
				}
			})

			logger.Info().
				Str("modules", strings.Join(args.DebugModules, ",")).
				Int("module_count", len(args.DebugModules)).
				Msg("Selective debug mode enabled - debug logging for specific modules only")
		} else {
			// Full verbose mode: --verbose without --debug-modules
			// All modules output debug logs (no filtering)

			// Enable debug level globally
			zerolog.SetGlobalLevel(zerolog.DebugLevel)

			// Enable debug for all key modules
			mutateLevelState(func(st *levelState) {
				st.selective = false
				st.debugModules = map[string]bool{}
				for module := range st.levels {
					st.levels[module] = zerolog.DebugLevel
				}
			})

			logger.Info().Msg("Full verbose mode enabled - debug logging for all modules")
		}
	} else if len(args.DebugModules) > 0 {
		// --debug-modules requires --verbose flag
		logger.Warn().Msg("--debug-modules requires --verbose flag. Ignoring debug modules configuration.")
	}

	return logger
}

// buildDevelopmentLogger creates a development-oriented logger configuration
// that writes formatted logs to stderr with debug level enabled
func buildDevelopmentLogger(_ *cliArgs.ParsedArgs, config *LoggerConfig) *Logger {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	// Default writer is console with masking
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr}
	var writer io.Writer = NewMaskingWriter(consoleWriter)

	// If debug log shipper is configured, create a multi-writer with masking
	if config.logShipper != nil {
		// Apply masking to the log shipper
		maskedShipper := NewMaskingWriter(config.logShipper)
		writer = zerolog.MultiLevelWriter(writer, maskedShipper)
		log.Printf("Debug log shipping enabled in development mode")
	}

	logger := zerolog.
		New(writer).
		With().
		Timestamp().
		Logger()
	return &logger
}

// buildProductionLogger creates a production-oriented logger configuration with:
// - Log rotation (10MB file size trigger)
// - Compression of rotated logs
// - Maximum of 5 backup files
// - 30-day retention period
// - Masking of sensitive information
// Console output is automatically added when running in interactive mode (run command)
func buildProductionLogger(args *cliArgs.ParsedArgs, config *LoggerConfig) *Logger {
	logPath := getLogPath()

	// Configure log rotation settings
	logRotator := &lumberjack.Logger{
		Filename:   logPath, // Path to the log file
		MaxSize:    10,      // Megabytes before rotation
		MaxBackups: 5,       // Number of backup files to keep
		MaxAge:     30,      // Days to keep backup files
		Compress:   true,    // Enable compression of rotated logs
	}

	// Define masked writers - start with log file
	writers := []io.Writer{NewMaskingWriter(logRotator)}

	// Detect if running in interactive mode (run command) vs service mode (daemon)
	isInteractive := service.Interactive()

	// Add console output in interactive mode (run command)
	// This ensures logs are visible in console when using: ./agent run
	if isInteractive {
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr}
		writers = append(writers, NewMaskingWriter(consoleWriter))
		log.Printf("Running in interactive mode - console output enabled")
	}

	// Add debug log shipper if configured
	if config.logShipper != nil {
		writers = append(writers, NewMaskingWriter(config.logShipper))
		log.Printf("Debug log shipping enabled in production mode")
	}

	// Set default production log level to info
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	// Create the multi-writer with all outputs
	logWriter := zerolog.MultiLevelWriter(writers...)

	// Create and configure the logger with timestamp
	logger := zerolog.
		New(logWriter).
		With().
		Timestamp().
		Logger()

	return &logger
}

// levelState is the immutable snapshot of the module-level
// configuration. Readers load it atomically on every Debug() call
// (allocation-free); writers copy-on-write under levelStateMu and swap
// the pointer. The previous package-level maps were mutated by the
// runtime HTTP log-level endpoint while every Debug() read them — a
// concurrent map read/write panic waiting to happen (audit C7, #274).
type levelState struct {
	levels       map[string]zerolog.Level
	selective    bool
	debugModules map[string]bool
}

var (
	// levelStateMu serializes WRITERS only; readers go through the
	// atomic pointer without taking it.
	levelStateMu sync.Mutex
	levelStatePo atomic.Pointer[levelState]
)

func init() {
	levelStatePo.Store(&levelState{
		levels:       defaultModuleLogLevels(),
		debugModules: map[string]bool{},
	})
}

func defaultModuleLogLevels() map[string]zerolog.Level {
	return map[string]zerolog.Level{
		"strategy.http":   zerolog.InfoLevel, // HTTP strategy logs
		"strategy.prtg":   zerolog.InfoLevel, // PRTG strategy logs
		"strategy.senhub": zerolog.InfoLevel, // SenHub strategy logs
		"probe.redfish":   zerolog.InfoLevel, // Redfish probe logs
		"probe.host":      zerolog.InfoLevel, // Host probes (CPU, memory, etc.)
		"probe.network":   zerolog.InfoLevel, // Network probes
		"probe.webapp":    zerolog.InfoLevel, // WebApp probes
		"probe.otel":      zerolog.InfoLevel, // OpenTelemetry probe
		"probe.gateway":   zerolog.InfoLevel, // Gateway probe
		"probe.veeam":     zerolog.InfoLevel, // Veeam Backup probe
		"probe.syslog":    zerolog.InfoLevel, // Syslog probe
		"cache":           zerolog.InfoLevel, // Cache operations
		"transformer":     zerolog.InfoLevel, // Metric transformers
		"scheduler":       zerolog.InfoLevel, // Probe scheduler
		"configuration":   zerolog.InfoLevel, // Configuration loading
	}
}

// mutateLevelState clones the current state, applies fn to the clone
// and swaps it in. Writers are rare (startup + the runtime log-level
// endpoint); readers never block.
func mutateLevelState(fn func(*levelState)) {
	levelStateMu.Lock()
	defer levelStateMu.Unlock()
	cur := levelStatePo.Load()
	next := &levelState{
		levels:       make(map[string]zerolog.Level, len(cur.levels)),
		selective:    cur.selective,
		debugModules: make(map[string]bool, len(cur.debugModules)),
	}
	for k, v := range cur.levels {
		next.levels[k] = v
	}
	for k, v := range cur.debugModules {
		next.debugModules[k] = v
	}
	fn(next)
	levelStatePo.Store(next)
}

// SetModuleLogLevel sets the log level for a specific module
func SetModuleLogLevel(module string, level zerolog.Level) {
	mutateLevelState(func(st *levelState) {
		st.levels[module] = level
	})
}

// SetModuleLogLevels sets multiple module log levels from configuration
func SetModuleLogLevels(configs []ModuleLogConfig) error {
	parsed := make(map[string]zerolog.Level, len(configs))
	for _, config := range configs {
		level, err := parseLogLevel(config.Level)
		if err != nil {
			return err
		}
		parsed[config.Module] = level
	}
	mutateLevelState(func(st *levelState) {
		for module, level := range parsed {
			st.levels[module] = level
		}
	})
	return nil
}

// parseLogLevel converts string level to zerolog.Level
func parseLogLevel(levelStr string) (zerolog.Level, error) {
	switch levelStr {
	case "debug":
		return zerolog.DebugLevel, nil
	case "info":
		return zerolog.InfoLevel, nil
	case "warn":
		return zerolog.WarnLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	case "fatal":
		return zerolog.FatalLevel, nil
	case "panic":
		return zerolog.PanicLevel, nil
	case "disabled":
		return zerolog.Disabled, nil
	default:
		return zerolog.InfoLevel, nil
	}
}

// NewModuleLogger creates a logger for a specific module with appropriate filtering
// GetModuleLogLevel returns the current log level for a module
func GetModuleLogLevel(module string) zerolog.Level {
	st := levelStatePo.Load()
	moduleLevel, exists := st.levels[module]
	if !exists {
		return zerolog.InfoLevel // Default level
	}
	return moduleLevel
}

// ModuleLogger wraps a zerolog.Logger with dynamic level checking for a specific module.
// Reads from global state on every call — no snapshots, no staling.
type ModuleLogger struct {
	*zerolog.Logger
	module string
}

func NewModuleLogger(baseLogger *Logger, module string) *ModuleLogger {
	// Nil-safe: callers (notably probe constructors driven by tests or
	// embedding code) may pass a nil base logger. Fall back to a no-op
	// logger instead of panicking before any work happens.
	if baseLogger == nil {
		nop := zerolog.Nop()
		baseLogger = &nop
	}

	logger := baseLogger.With().
		Str("module", module).
		Logger()

	return &ModuleLogger{
		Logger: &logger,
		module: module,
	}
}

// isModuleEnabled checks if a module should output debug logs.
// Supports prefix matching: "probe" matches "probe.veeam", "probe.citrix", etc.
func isModuleEnabled(st *levelState, module string) bool {
	if st.debugModules[module] {
		return true
	}
	for prefix := range st.debugModules {
		if strings.HasPrefix(module, prefix+".") {
			return true
		}
	}
	return false
}

// Debug logs a debug message if the module's current level allows it
func (m *ModuleLogger) Debug() *zerolog.Event {
	st := levelStatePo.Load()
	// In selective debug mode, only allow debug logs for enabled modules (with prefix matching)
	if st.selective {
		if !isModuleEnabled(st, m.module) {
			disabledLogger := m.Logger.Level(zerolog.Disabled)
			return disabledLogger.Debug()
		}
	}

	// Check module log level (unknown modules default to Info, which
	// keeps Debug disabled — same contract as GetModuleLogLevel).
	level, ok := st.levels[m.module]
	if !ok {
		level = zerolog.InfoLevel
	}
	if level <= zerolog.DebugLevel {
		return m.Logger.Debug()
	}

	disabledLogger := m.Logger.Level(zerolog.Disabled)
	return disabledLogger.Debug()
}

// Info logs an info message (always enabled for all modules)
func (m *ModuleLogger) Info() *zerolog.Event {
	// Info level is never filtered - all modules can log info messages
	return m.Logger.Info()
}

// Warn logs a warning message (always enabled for all modules)
func (m *ModuleLogger) Warn() *zerolog.Event {
	// Warn level is never filtered - all modules can log warnings
	return m.Logger.Warn()
}

// Error logs an error message (always enabled for all modules)
func (m *ModuleLogger) Error() *zerolog.Event {
	// Error level is never filtered - all modules can log errors
	return m.Logger.Error()
}

// GetModuleLogLevels returns current module log level configuration
func GetModuleLogLevels() map[string]zerolog.Level {
	st := levelStatePo.Load()
	result := make(map[string]zerolog.Level, len(st.levels))
	for k, v := range st.levels {
		result[k] = v
	}
	return result
}

// ModuleInfo describes a debug module with its category
type ModuleInfo struct {
	Name     string
	Category string
}

// GetAvailableModules returns all available debug modules with categories
func GetAvailableModulesInfo() []ModuleInfo {
	return []ModuleInfo{
		// Agent core
		{"sensor", "Agent Core"},
		{"server", "Agent Core"},
		{"data_store", "Agent Core"},
		{"service.auto_update", "Agent Core"},

		// Configuration
		{"configuration.local", "Configuration"},
		{"configuration.remote", "Configuration"},
		{"configuration.agent", "Configuration"},
		{"configuration.migrator", "Configuration"},

		// Strategies
		{"strategy.http", "Strategies"},
		{"strategy.prtg", "Strategies"},
		{"strategy.senhub", "Strategies"},
		{"strategy.event", "Strategies"},

		// Transformers & cache
		{"transformer", "Data Processing"},
		{"transformer.definition", "Data Processing"},
		{"lookups", "Data Processing"},
		{"status.service", "Data Processing"},
		{"status.helper", "Data Processing"},
		{"status.cache_adapter", "Data Processing"},

		// System probes
		{"probe.cpu", "System Probes"},
		{"probe.memory", "System Probes"},
		{"probe.network", "System Probes"},
		{"probe.logicaldisk", "System Probes"},
		{"probe.wifi", "System Probes"},
		{"probe.host", "System Probes (Windows)"},

		// Infrastructure probes
		{"probe.netscaler", "Infrastructure Probes"},
		{"probe.redfish", "Infrastructure Probes"},
		{"probe.redfish.client", "Infrastructure Probes"},
		{"probe.veeam", "Infrastructure Probes"},
		{"probe.veeam.client", "Infrastructure Probes"},

		// Application probes
		{"probe.citrix", "Application Probes"},
		{"probe.citrix.client", "Application Probes"},
		{"probe.citrix.ddc", "Application Probes"},
		{"probe.citrix.filters", "Application Probes"},
		{"probe.citrix.inventory", "Application Probes"},
		{"probe.citrix.metrics", "Application Probes"},
		{"probe.citrix.common", "Application Probes"},
		{"probe.webapp", "Application Probes"},
		{"probe.loadwebapp", "Application Probes"},
		{"probe.gateway", "Application Probes"},

		// Event probes
		{"probe.syslog", "Event Probes"},
		{"probe.event", "Event Probes"},
		{"probe.otel", "Event Probes"},

		// Platform specific
		{"pdh.windows", "Platform Specific"},
	}
}

// GetAvailableModules returns a flat list of module names (backward compat)
func GetAvailableModules() []string {
	modules := GetAvailableModulesInfo()
	names := make([]string, len(modules))
	for i, m := range modules {
		names[i] = m.Name
	}
	return names
}
