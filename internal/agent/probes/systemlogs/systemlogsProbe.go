package systemlogs

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
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
		
	return &SystemLogsProbe{
		rawConfig:      config,
		config:         parsedConfig,
		logger:         &localLogger,
		handles:        make(map[LogSource][]interface{}),
		lastCollection: time.Now(),
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
			cfg.WindowsSettings.Channels = []string{"Application", "System"} // Default channels
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
			cfg.WindowsSettings.Levels = []string{"Critical", "Error", "Warning"} // Default levels
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
	return startImpl(p, quitChannel)
}

// OnShutdown cleans up resources when the SystemLogsProbe is stopped
func (p *SystemLogsProbe) OnShutdown(ctx context.Context) error {
	return shutdownImpl(p, ctx)
}

// processEvent converts a system log event to a DataPoint
func (p *SystemLogsProbe) processEvent(event SystemLogEvent) data_store.DataPoint {
	// Create standard set of tags
	eventTags := []tags.Tag{
		{Key: "source", Value: event.Source, Private: false},
		{Key: "id", Value: event.ID, Private: false},
		{Key: "level", Value: event.Level, Private: false},
		{Key: "message", Value: event.Message, Private: false},
	}

	// Add any additional metadata as tags
	for key, value := range event.Metadata {
		eventTags = append(eventTags, tags.Tag{
			Key:     key,
			Value:   value,
			Private: false,
		})
	}

	p.logger.Debug().
		Time("timestamp", event.Timestamp).
		Str("source", event.Source).
		Str("id", event.ID).
		Str("level", event.Level).
		Msg("Collected system log entry")

	return data_store.DataPoint{
		Name:      "systemlogs_event",
		Timestamp: event.Timestamp,
		Value:     1.0, // Event count is always 1
		Tags:      eventTags,
	}
}

// String returns a string representation of the SystemLogsProbe
func (p *SystemLogsProbe) String() string {
	return fmt.Sprintf("SystemLogsProbe{sources=%v}", p.config.Sources)
}