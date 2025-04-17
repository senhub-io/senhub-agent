// Package systemlogs provides system log collection functionality
package systemlogs

import (
	"context"
	"runtime"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// Default values
const (
	DefaultInterval = 60 * time.Second
	DefaultMaxEvents = 100
	MaxSubscriptions = 10
)

// LogSource represents the type of system logs to collect
type LogSource string

const (
	LogSourceWindowsEvent LogSource = "windowsevents" // Windows Event Log
	LogSourceJournald     LogSource = "journald"     // Linux systemd journal
	LogSourceSyslog       LogSource = "syslog"       // Traditional syslog
	LogSourceASL          LogSource = "asl"          // Apple System Log / Unified Logging
)

// SystemLogEvent represents a generic system log entry
type SystemLogEvent struct {
	Source    string    // Source of the log (application, service name)
	ID        string    // Event ID or identifier 
	Level     string    // Severity level
	Message   string    // Log message content
	Timestamp time.Time // When the event occurred
	Metadata  map[string]string // Additional platform-specific metadata
}

// SystemLogsProbeConfig holds the configuration for the SystemLogs Probe
type SystemLogsProbeConfig struct {
	// Sources to monitor (automatically determined if empty)
	Sources []LogSource
	// Source-specific settings
	WindowsSettings struct {
		Channels []string // Event log channels to monitor
		EventIDs []int    // Event IDs to filter
		Levels   []string // Severity levels to include
	}
	JournaldSettings struct {
		Units    []string // Systemd units to monitor
		Priority []string // Priority levels (emerg, alert, crit, etc)
	}
	// Common settings
	MaxEvents int           // Maximum number of events to collect per interval
	Interval  time.Duration // Collection interval
	Filter    string        // Optional filter expression 
}

// SystemLogsProbe represents a probe that collects system logs
type SystemLogsProbe struct {
	rawConfig      map[string]interface{}
	config         SystemLogsProbeConfig
	logger         *logger.Logger
	callback       func([]data_store.DataPoint) error
	handles        map[LogSource][]interface{} // Platform-specific handles
	lastCollection time.Time
}

// SetCallback sets the callback function for the SystemLogsProbe
func (p *SystemLogsProbe) SetCallback(callback func([]data_store.DataPoint) error) {
	p.callback = callback
}

// NewSystemLogsProbe creates a new instance of SystemLogsProbe
func NewSystemLogsProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	parsedConfig, err := parseSystemLogsProbeConfig(config)
	if err != nil {
		return nil, err
	}

	localLogger := logger.With().
		Interface("sources", parsedConfig.Sources).
		Int("maxEvents", parsedConfig.MaxEvents).
		Logger()

	localLogger.Debug().
		Any("config", parsedConfig).
		Msg("Creating new SystemLogs probe")
		
	// Initialize with a lookback period to collect recent events
	// This ensures we collect events that happened before the probe started
	initialLookback := time.Hour * 24 // Default to 24 hour lookback for first collection
	if lookbackVal, ok := config["initial_lookback"].(float64); ok {
		initialLookback = time.Duration(lookbackVal) * time.Minute
	}

	return &SystemLogsProbe{
		rawConfig:      config,
		config:         parsedConfig,
		logger:         &localLogger,
		handles:        make(map[LogSource][]interface{}),
		lastCollection: time.Now().Add(-initialLookback),
	}, nil
}

// parseSystemLogsProbeConfig parses the configuration for the SystemLogsProbe
func parseSystemLogsProbeConfig(config map[string]interface{}) (SystemLogsProbeConfig, error) {
	var cfg SystemLogsProbeConfig
	var err error

	// Set default values
	cfg.MaxEvents = DefaultMaxEvents
	cfg.Interval = DefaultInterval

	// Auto-determine sources based on platform if not specified
	if sourcesVal, ok := config["sources"].([]interface{}); ok {
		for _, src := range sourcesVal {
			if srcStr, ok := src.(string); ok {
				cfg.Sources = append(cfg.Sources, LogSource(srcStr))
			}
		}
	}
	
	// If no sources specified, use defaults for the current OS
	if len(cfg.Sources) == 0 {
		switch runtime.GOOS {
		case "windows":
			cfg.Sources = []LogSource{LogSourceWindowsEvent}
		case "linux":
			cfg.Sources = []LogSource{LogSourceJournald}
		case "darwin":
			cfg.Sources = []LogSource{LogSourceASL}
		default:
			cfg.Sources = []LogSource{LogSourceSyslog}
		}
	}

	// Parse Windows-specific settings if present
	if winConfig, ok := config["windows"].(map[string]interface{}); ok {
		// Parse channels
		if channelsVal, ok := winConfig["channels"].([]interface{}); ok {
			for _, ch := range channelsVal {
				if chStr, ok := ch.(string); ok {
					cfg.WindowsSettings.Channels = append(cfg.WindowsSettings.Channels, chStr)
				}
			}
		}
		if len(cfg.WindowsSettings.Channels) == 0 {
			cfg.WindowsSettings.Channels = []string{"Application", "System", "Security"} // Default channels
		}

		// Parse event IDs
		if eventIDsVal, ok := winConfig["event_ids"].([]interface{}); ok {
			for _, id := range eventIDsVal {
				if idFloat, ok := id.(float64); ok {
					cfg.WindowsSettings.EventIDs = append(cfg.WindowsSettings.EventIDs, int(idFloat))
				}
			}
		}

		// Parse levels
		if levelsVal, ok := winConfig["levels"].([]interface{}); ok {
			for _, level := range levelsVal {
				if levelStr, ok := level.(string); ok {
					cfg.WindowsSettings.Levels = append(cfg.WindowsSettings.Levels, levelStr)
				}
			}
		}
		if len(cfg.WindowsSettings.Levels) == 0 {
			cfg.WindowsSettings.Levels = []string{"Critical", "Error", "Warning", "Information"} // Include Information level by default
		}
	}

	// Parse Journald-specific settings if present
	if journalConfig, ok := config["journald"].(map[string]interface{}); ok {
		// Parse units
		if unitsVal, ok := journalConfig["units"].([]interface{}); ok {
			for _, unit := range unitsVal {
				if unitStr, ok := unit.(string); ok {
					cfg.JournaldSettings.Units = append(cfg.JournaldSettings.Units, unitStr)
				}
			}
		}

		// Parse priority levels
		if priorityVal, ok := journalConfig["priority"].([]interface{}); ok {
			for _, prio := range priorityVal {
				if prioStr, ok := prio.(string); ok {
					cfg.JournaldSettings.Priority = append(cfg.JournaldSettings.Priority, prioStr)
				}
			}
		}
		if len(cfg.JournaldSettings.Priority) == 0 {
			// Default to error and above
			cfg.JournaldSettings.Priority = []string{"emerg", "alert", "crit", "err"}
		}
	}

	// Parse common settings
	if maxEventsVal, ok := config["max_events"].(float64); ok {
		cfg.MaxEvents = int(maxEventsVal)
		if cfg.MaxEvents <= 0 {
			cfg.MaxEvents = DefaultMaxEvents
		}
	}

	if intervalVal, ok := config["interval"].(float64); ok {
		cfg.Interval = time.Duration(intervalVal) * time.Second
		if cfg.Interval < 10*time.Second {
			cfg.Interval = 10 * time.Second // Minimum interval
		}
	}

	if filterVal, ok := config["filter"].(string); ok {
		cfg.Filter = filterVal
	}

	return cfg, err
}

// GetName returns the name of the SystemLogsProbe
func (p *SystemLogsProbe) GetName() string {
	return "systemlogs"
}

// GetTargetStrategies returns the target strategies for the SystemLogsProbe
func (p *SystemLogsProbe) GetTargetStrategies() []string {
	return []string{"event"}
}

// ShouldStart indicates whether the SystemLogsProbe should start
func (p *SystemLogsProbe) ShouldStart() bool {
	// Check if we have any sources that can be collected on this OS
	for _, source := range p.config.Sources {
		if isSourceSupported(source) {
			return true
		}
	}
	return false
}

// isSourceSupported checks if a log source is supported on the current platform
// Implemented in platform-specific files

// GetInterval returns the collection interval for the SystemLogsProbe
func (p *SystemLogsProbe) GetInterval() time.Duration {
	return p.config.Interval
}

// Platform-specific implementation functions
var (
	collectImpl func(*SystemLogsProbe) ([]data_store.DataPoint, error)
	startImpl func(*SystemLogsProbe, chan struct{}) error
	shutdownImpl func(*SystemLogsProbe, context.Context) error
)

// Collect gathers system logs and returns them as DataPoints
func (p *SystemLogsProbe) Collect() ([]data_store.DataPoint, error) {
	return collectImpl(p)
}

// OnStart initializes the SystemLogsProbe
func (p *SystemLogsProbe) OnStart(quitChannel chan struct{}) error {
	// Log the time from which we'll start collecting events
	p.logger.Info().
		Time("collectingSince", p.lastCollection).
		Msg("Starting systemlogs probe")
		
	return startImpl(p, quitChannel)
}

// OnShutdown cleans up resources when the SystemLogsProbe is stopped
func (p *SystemLogsProbe) OnShutdown(ctx context.Context) error {
	return shutdownImpl(p, ctx)
}

// ProcessEvent converts a system log event to a DataPoint (exported for testing)
func (p *SystemLogsProbe) ProcessEvent(event SystemLogEvent) data_store.DataPoint {
	// Map severity levels to standard format
	severity := mapToStandardSeverity(event.Level)
	
	// Use computer name from Windows event when available, otherwise fall back to system hostname
	var hostname string
	
	// First check if we have hostname in metadata (from Windows Computer field)
	hostname = event.Metadata["hostname"]
	
	// If hostname is empty or "Unknown", fall back to system hostname
	if hostname == "" || hostname == "Unknown" {
		hostTags, err := common.GetHostTags()
		if err != nil {
			// Fallback if we can't get real hostname
			p.logger.Error().Err(err).Msg("Failed to get host information")
			// Use source as last resort
			hostname = event.Source
		} else {
			// Find the hostname from host tags
			for _, tag := range hostTags {
				if tag.Key == "host" {
					hostname = tag.Value
					break
				}
			}
		}
	}
	
	// Create standard set of tags with required fields for event strategy
	eventTags := []tags.Tag{
		// Required fields for event strategy
		{Key: "host", Value: hostname, Private: false},
		{Key: "severity", Value: severity, Private: false},
		{Key: "message", Value: event.Message, Private: false},
		
		// Additional fields specific to system logs
		{Key: "event_source", Value: event.Source, Private: false},
		{Key: "event_id", Value: event.ID, Private: false},
		{Key: "event_level", Value: event.Level, Private: false},
	}

	// Add any additional metadata as tags
	for key, value := range event.Metadata {
		// Skip hostname if already added as host
		if key == "hostname" {
			continue
		}
		eventTags = append(eventTags, tags.Tag{
			Key:     key,
			Value:   value,
			Private: false,
		})
	}

	p.logger.Info().
		Time("timestamp", event.Timestamp).
		Str("host", hostname).
		Str("severity", severity).
		Str("source", event.Source).
		Str("id", event.ID).
		Str("level", event.Level).
		Str("message", event.Message).
		Msg("System log entry ready for sending to server")

	return data_store.DataPoint{
		Name:      "systemlogs_event",
		Timestamp: event.Timestamp,
		// No value needed for event data points
		Value:     0,
		Tags:      eventTags,
	}
}

// mapToStandardSeverity converts various severity/level formats to standard syslog severity
func mapToStandardSeverity(level string) string {
	// Normalize level to lowercase for case-insensitive comparison
	levelLower := strings.ToLower(level)
	
	// Map Windows event levels to syslog severity
	switch levelLower {
	case "critical":
		return "2" // Critical
	case "error":
		return "3" // Error
	case "warning":
		return "4" // Warning
	case "information", "info":
		return "6" // Informational
	case "verbose", "debug":
		return "7" // Debug
	}
	
	// Map Linux/journald priorities if they match pattern
	switch levelLower {
	case "emerg", "emergency":
		return "0" // Emergency
	case "alert":
		return "1" // Alert
	case "crit":
		return "2" // Critical
	case "err":
		return "3" // Error
	case "warning", "warn":
		return "4" // Warning
	case "notice":
		return "5" // Notice
	case "info":
		return "6" // Informational
	case "debug":
		return "7" // Debug
	}
	
	// Default to Notice if unknown
	return "5"
}

// String returns a string representation of the SystemLogsProbe
func (p *SystemLogsProbe) String() string {
	sourcesStr := ""
	for i, src := range p.config.Sources {
		if i > 0 {
			sourcesStr += ","
		}
		sourcesStr += string(src)
	}
	return "SystemLogsProbe{sources=[" + sourcesStr + "]}"
}