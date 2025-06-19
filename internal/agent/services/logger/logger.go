package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"

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

// getLogPath determines the appropriate log file location based on the operating system.
// It follows OS-specific conventions for log placement:
// - Windows: %ProgramData%\SenHub\logs
// - macOS: /Library/Logs/SenHub
// - Linux/Unix: /var/log/senhub
// If the desired location is not writable, it falls back to the executable's directory
func getLogPath() string {
	var basePath string

	switch runtime.GOOS {
	case "windows":
		// For Windows, try to use %ProgramData% directory first
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		basePath = filepath.Join(programData, "SenHub", "logs")
	case "darwin":
		// For macOS, use the standard system logs directory
		basePath = "/Library/Logs/SenHub"
	default:
		// For Linux/Unix systems, use the conventional /var/log directory
		basePath = "/var/log/senhub"
	}

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

	log.Printf("Using log file: %s", logPath)
	return logPath
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

	// Initialize selective debug mode variables
	selectiveDebugMode = false
	activeDebugModules = make(map[string]bool)

	// Configure debug logging
	if len(args.DebugModules) > 0 && !args.Verbose {
		// Selective debug mode: only enable debug for specified modules (unless --verbose is also set)
		selectiveDebugMode = true
		activeDebugModules = make(map[string]bool)

		// In selective mode, set global level to DEBUG to allow debug logs for enabled modules
		// ModuleLoggers will filter all log levels based on module configuration
		zerolog.SetGlobalLevel(zerolog.DebugLevel)

		// Enable debug only for specified modules
		for _, module := range args.DebugModules {
			SetModuleLogLevel(module, zerolog.DebugLevel)
			activeDebugModules[module] = true
		}

		logger.Info().
			Strs("modules", args.DebugModules).
			Int("module_count", len(args.DebugModules)).
			Bool("verbose_flag", args.Verbose).
			Msg("Selective debug mode enabled - debug logging activated for specific modules only")
	} else if args.Verbose || len(args.DebugModules) > 0 {
		// Full verbose mode: enable debug globally (when --verbose is set, or both --verbose and --debug-modules)
		selectiveDebugMode = false
		activeDebugModules = make(map[string]bool) // Empty map instead of nil
		zerolog.SetGlobalLevel(zerolog.DebugLevel)

		// Also enable debug for key modules
		SetModuleLogLevel("strategy.http", zerolog.DebugLevel)
		SetModuleLogLevel("cache", zerolog.DebugLevel)
		SetModuleLogLevel("probe.redfish", zerolog.DebugLevel)
		SetModuleLogLevel("configuration", zerolog.DebugLevel)
		SetModuleLogLevel("scheduler", zerolog.DebugLevel)

		if args.Verbose && len(args.DebugModules) > 0 {
			logger.Info().
				Strs("modules", args.DebugModules).
				Msg("Full verbose mode enabled with focus on specific modules - debug logging activated for all components")
		} else {
			logger.Info().Msg("Full verbose mode enabled - debug logging activated for all components")
		}
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
// If verbose mode is enabled, it will also output to stderr alongside the file
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

	// Add console output if verbose or debug modules are specified
	if args.Verbose || len(args.DebugModules) > 0 {
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr}
		writers = append(writers, NewMaskingWriter(consoleWriter))
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

// Global module levels configuration
var moduleLogLevels = map[string]zerolog.Level{
	"strategy.http":   zerolog.InfoLevel, // HTTP strategy logs
	"strategy.prtg":   zerolog.InfoLevel, // PRTG strategy logs
	"strategy.senhub": zerolog.InfoLevel, // SenHub strategy logs
	"probe.redfish":   zerolog.InfoLevel, // Redfish probe logs
	"probe.host":      zerolog.InfoLevel, // Host probes (CPU, memory, etc.)
	"probe.network":   zerolog.InfoLevel, // Network probes
	"probe.webapp":    zerolog.InfoLevel, // WebApp probes
	"probe.otel":      zerolog.InfoLevel, // OpenTelemetry probe
	"probe.gateway":   zerolog.InfoLevel, // Gateway probe
	"probe.syslog":    zerolog.InfoLevel, // Syslog probe
	"cache":           zerolog.InfoLevel, // Cache operations
	"transformer":     zerolog.InfoLevel, // Metric transformers
	"scheduler":       zerolog.InfoLevel, // Probe scheduler
	"configuration":   zerolog.InfoLevel, // Configuration loading
}

// Selective debug mode tracking
var selectiveDebugMode bool
var activeDebugModules map[string]bool

// SetModuleLogLevel sets the log level for a specific module
func SetModuleLogLevel(module string, level zerolog.Level) {
	moduleLogLevels[module] = level
}

// SetModuleLogLevels sets multiple module log levels from configuration
func SetModuleLogLevels(configs []ModuleLogConfig) error {
	for _, config := range configs {
		level, err := parseLogLevel(config.Level)
		if err != nil {
			return err
		}
		moduleLogLevels[config.Module] = level
	}
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
	moduleLevel, exists := moduleLogLevels[module]
	if !exists {
		return zerolog.InfoLevel // Default level
	}
	return moduleLevel
}

// ModuleLogger wraps a zerolog.Logger with dynamic level checking for a specific module
type ModuleLogger struct {
	*zerolog.Logger
	module         string
	selectiveMode  bool
	enabledModules map[string]bool
}

func NewModuleLogger(baseLogger *Logger, module string) *ModuleLogger {
	// Create logger with module context
	logger := baseLogger.With().
		Str("module", module).
		Logger()

	return &ModuleLogger{
		Logger:         &logger,
		module:         module,
		selectiveMode:  selectiveDebugMode,
		enabledModules: copyMap(activeDebugModules),
	}
}

// copyMap creates a copy of the activeDebugModules map to avoid shared state issues
func copyMap(original map[string]bool) map[string]bool {
	if original == nil {
		return make(map[string]bool)
	}
	copy := make(map[string]bool)
	for k, v := range original {
		copy[k] = v
	}
	return copy
}

// Debug logs a debug message if the module's current level allows it
func (m *ModuleLogger) Debug() *zerolog.Event {
	// In selective debug mode, only allow debug logs for specifically enabled modules
	if m.selectiveMode {
		if _, enabled := m.enabledModules[m.module]; !enabled {
			disabledLogger := m.Logger.Level(zerolog.Disabled)
			return disabledLogger.Debug()
		}
	}

	// Check module log level for normal mode or enabled modules
	if GetModuleLogLevel(m.module) <= zerolog.DebugLevel {
		return m.Logger.Debug()
	}

	disabledLogger := m.Logger.Level(zerolog.Disabled)
	return disabledLogger.Debug()
}

// Info logs an info message (filtered in selective debug mode)
func (m *ModuleLogger) Info() *zerolog.Event {
	// In selective debug mode, only allow logs for specifically enabled modules
	if m.selectiveMode {
		if _, enabled := m.enabledModules[m.module]; !enabled {
			disabledLogger := m.Logger.Level(zerolog.Disabled)
			return disabledLogger.Info()
		}
	}
	return m.Logger.Info()
}

// Warn logs a warning message (filtered in selective debug mode)
func (m *ModuleLogger) Warn() *zerolog.Event {
	// In selective debug mode, only allow logs for specifically enabled modules
	if m.selectiveMode {
		if _, enabled := m.enabledModules[m.module]; !enabled {
			disabledLogger := m.Logger.Level(zerolog.Disabled)
			return disabledLogger.Warn()
		}
	}
	return m.Logger.Warn()
}

// Error logs an error message (filtered in selective debug mode)
func (m *ModuleLogger) Error() *zerolog.Event {
	// In selective debug mode, only allow logs for specifically enabled modules
	if m.selectiveMode {
		if _, enabled := m.enabledModules[m.module]; !enabled {
			disabledLogger := m.Logger.Level(zerolog.Disabled)
			return disabledLogger.Error()
		}
	}
	return m.Logger.Error()
}

// GetModuleLogLevels returns current module log level configuration
func GetModuleLogLevels() map[string]zerolog.Level {
	result := make(map[string]zerolog.Level)
	for k, v := range moduleLogLevels {
		result[k] = v
	}
	return result
}

// GetAvailableModules returns a list of all available debug modules
func GetAvailableModules() []string {
	return []string{
		// Core services
		"configuration",
		"scheduler",
		"cache",
		"transformer",
		"sensor",
		"auto_update",

		// Data storage strategies
		"strategy.http",
		"strategy.prtg",
		"strategy.senhub",
		"strategy.event",

		// System probes
		"probe.cpu",
		"probe.memory",
		"probe.network",
		"probe.logicaldisk",
		"probe.host",

		// Application probes
		"probe.webapp",
		"probe.gateway",
		"probe.syslog",
		"probe.event",
		"probe.otel",
		"probe.redfish",

		// Platform-specific
		"pdh.windows",

		// Sub-modules (examples)
		"probe.redfish.client",
		"data_store",
	}
}
