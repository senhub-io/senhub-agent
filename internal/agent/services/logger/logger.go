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
	logFile      io.WriteCloser
	logShipper   io.Writer
	debugConfig  *debugshipper.Config
	moduleLevels map[string]zerolog.Level // Per-module log levels
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

	// Attempt to create the log directory if it doesn't exist
	// This will also test if we have sufficient permissions
	if err := os.MkdirAll(basePath, 0755); err != nil {
		log.Printf("Unable to create log directory: %v", err)
		// Fall back to executable directory if we can't write to the preferred location
		exePath, _ := os.Executable()
		basePath = filepath.Dir(exePath)
	}

	return filepath.Join(basePath, "senhubagent.log")
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

	// Enable debug level logging if verbose mode is requested
	if args.Verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		
		// If specific debug modules are specified, only enable those
		if len(args.DebugModules) > 0 {
			// Enable debug only for specified modules
			for _, module := range args.DebugModules {
				SetModuleLogLevel(module, zerolog.DebugLevel)
			}
			logger.Info().
				Strs("modules", args.DebugModules).
				Msg("Verbose mode enabled - debug logging activated for specific modules")
		} else {
			// Keep existing behavior: enable debug for all key modules
			SetModuleLogLevel("strategy.http", zerolog.DebugLevel)
			SetModuleLogLevel("cache", zerolog.DebugLevel)
			SetModuleLogLevel("probe.redfish", zerolog.DebugLevel)
			SetModuleLogLevel("configuration", zerolog.DebugLevel)
			SetModuleLogLevel("scheduler", zerolog.DebugLevel)
			
			logger.Info().Msg("Verbose mode enabled - debug logging activated for all key modules")
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

	// Add console output if verbose
	if args.Verbose {
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr}
		writers = append(writers, NewMaskingWriter(consoleWriter))
	}

	// Add debug log shipper if configured
	if config.logShipper != nil {
		writers = append(writers, NewMaskingWriter(config.logShipper))
		log.Printf("Debug log shipping enabled in production mode")
	}

	// Set default production log level to warn
	zerolog.SetGlobalLevel(zerolog.WarnLevel)

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
	"strategy.http":      zerolog.InfoLevel,  // HTTP strategy logs
	"strategy.prtg":      zerolog.InfoLevel,  // PRTG strategy logs  
	"strategy.senhub":    zerolog.InfoLevel,  // SenHub strategy logs
	"probe.redfish":      zerolog.InfoLevel,  // Redfish probe logs
	"probe.host":         zerolog.InfoLevel,  // Host probes (CPU, memory, etc.)
	"probe.network":      zerolog.InfoLevel,  // Network probes
	"probe.webapp":       zerolog.InfoLevel,  // WebApp probes
	"probe.otel":         zerolog.InfoLevel,  // OpenTelemetry probe
	"probe.gateway":      zerolog.InfoLevel,  // Gateway probe
	"probe.syslog":       zerolog.InfoLevel,  // Syslog probe
	"cache":              zerolog.InfoLevel,  // Cache operations
	"transformer":        zerolog.InfoLevel,  // Metric transformers
	"scheduler":          zerolog.InfoLevel,  // Probe scheduler
	"configuration":      zerolog.InfoLevel,  // Configuration loading
}

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
	module string
}

func NewModuleLogger(baseLogger *Logger, module string) *ModuleLogger {
	// Create logger with module context
	logger := baseLogger.With().
		Str("module", module).
		Logger()
	
	return &ModuleLogger{
		Logger: &logger,
		module: module,
	}
}

// Debug logs a debug message if the module's current level allows it
func (m *ModuleLogger) Debug() *zerolog.Event {
	if GetModuleLogLevel(m.module) <= zerolog.DebugLevel {
		return m.Logger.Debug()
	}
	// Create a disabled logger for suppressing output
	disabledLogger := m.Logger.Level(zerolog.Disabled)
	return disabledLogger.Debug()
}

// Info logs an info message if the module's current level allows it
func (m *ModuleLogger) Info() *zerolog.Event {
	if GetModuleLogLevel(m.module) <= zerolog.InfoLevel {
		return m.Logger.Info()
	}
	disabledLogger := m.Logger.Level(zerolog.Disabled)
	return disabledLogger.Info()
}

// Warn logs a warning message if the module's current level allows it
func (m *ModuleLogger) Warn() *zerolog.Event {
	if GetModuleLogLevel(m.module) <= zerolog.WarnLevel {
		return m.Logger.Warn()
	}
	disabledLogger := m.Logger.Level(zerolog.Disabled)
	return disabledLogger.Warn()
}

// Error logs an error message if the module's current level allows it
func (m *ModuleLogger) Error() *zerolog.Event {
	if GetModuleLogLevel(m.module) <= zerolog.ErrorLevel {
		return m.Logger.Error()
	}
	disabledLogger := m.Logger.Level(zerolog.Disabled)
	return disabledLogger.Error()
}

// GetModuleLogLevels returns current module log level configuration
func GetModuleLogLevels() map[string]zerolog.Level {
	result := make(map[string]zerolog.Level)
	for k, v := range moduleLogLevels {
		result[k] = v
	}
	return result
}
