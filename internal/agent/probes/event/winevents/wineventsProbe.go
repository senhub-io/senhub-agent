package winevents

import (
	"context"
	"fmt"
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

// WinEventProbeConfig holds the configuration for the Windows Event Log Probe
type WinEventProbeConfig struct {
	// Event log channels to monitor (Application, System, Security, etc.)
	Channels []string
	// Event IDs to filter, empty means all events
	EventIDs []int
	// Levels to include (Critical, Error, Warning, Information, Verbose)
	Levels []string
	// Maximum number of events to collect per interval
	MaxEvents int
	// Collection interval
	Interval time.Duration
}

// WinEventProbe represents a probe that collects Windows Event Log entries
type WinEventProbe struct {
	rawConfig      map[string]interface{}
	config         WinEventProbeConfig
	logger         *logger.Logger
	callback       func([]data_store.DataPoint) error
	queryHandles   []interface{} // placeholder for Windows event query handles
	lastCollection time.Time
}

// SetCallback sets the callback function for the WinEventProbe
func (p *WinEventProbe) SetCallback(callback func([]data_store.DataPoint) error) {
	p.callback = callback
}

// NewWinEventProbe creates a new instance of WinEventProbe
func NewWinEventProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	parsedConfig, err := parseWinEventProbeConfig(config)
	if err != nil {
		return nil, err
	}

	localLogger := logger.With().
		Interface("channels", parsedConfig.Channels).
		Interface("eventIDs", parsedConfig.EventIDs).
		Interface("levels", parsedConfig.Levels).
		Int("maxEvents", parsedConfig.MaxEvents).
		Logger()

	localLogger.Debug().
		Any("config", parsedConfig).
		Msg("Creating new Windows Event Log probe")
		
	return &WinEventProbe{
		rawConfig:      config,
		config:         parsedConfig,
		logger:         &localLogger,
		lastCollection: time.Now(),
	}, nil
}

// parseWinEventProbeConfig parses the configuration for the WinEventProbe
func parseWinEventProbeConfig(config map[string]interface{}) (WinEventProbeConfig, error) {
	var err error
	var channels []string
	var eventIDs []int
	var levels []string
	maxEvents := DefaultMaxEvents
	interval := DefaultInterval

	// Extract channels configuration
	if channelsVal, ok := config["channels"].([]interface{}); ok {
		for _, ch := range channelsVal {
			if chStr, ok := ch.(string); ok {
				channels = append(channels, chStr)
			}
		}
	}
	if len(channels) == 0 {
		channels = []string{"Application", "System"} // Default channels
	}

	// Extract event IDs configuration
	if eventIDsVal, ok := config["event_ids"].([]interface{}); ok {
		for _, id := range eventIDsVal {
			if idFloat, ok := id.(float64); ok {
				eventIDs = append(eventIDs, int(idFloat))
			}
		}
	}

	// Extract levels configuration
	if levelsVal, ok := config["levels"].([]interface{}); ok {
		for _, level := range levelsVal {
			if levelStr, ok := level.(string); ok {
				levels = append(levels, levelStr)
			}
		}
	}
	if len(levels) == 0 {
		levels = []string{"Critical", "Error", "Warning"} // Default levels
	}

	// Extract max events configuration
	if maxEventsVal, ok := config["max_events"].(float64); ok {
		maxEvents = int(maxEventsVal)
		if maxEvents <= 0 {
			maxEvents = DefaultMaxEvents
		}
	}

	// Extract interval configuration
	if intervalVal, ok := config["interval"].(float64); ok {
		interval = time.Duration(intervalVal) * time.Second
		if interval < 10*time.Second {
			interval = 10 * time.Second // Minimum interval
		}
	}

	if len(channels) > MaxSubscriptions {
		err = fmt.Errorf("too many channels specified, maximum is %d", MaxSubscriptions)
	}

	return WinEventProbeConfig{
		Channels:  channels,
		EventIDs:  eventIDs,
		Levels:    levels,
		MaxEvents: maxEvents,
		Interval:  interval,
	}, err
}

// GetName returns the name of the WinEventProbe
func (p *WinEventProbe) GetName() string {
	return "winevents"
}

// ShouldStart indicates whether the WinEventProbe should start
func (p *WinEventProbe) ShouldStart() bool {
	// This probe should only run on Windows
	return isWindows()
}

// isWindows is implemented in platform-specific files
// wineventsProbe_windows.go for Windows
// wineventsProbe_nonwindows.go for other platforms

// GetInterval returns the collection interval for the WinEventProbe
func (p *WinEventProbe) GetInterval() time.Duration {
	return p.config.Interval
}

// Platform-specific implementation functions
var (
	collectImpl func(*WinEventProbe) ([]data_store.DataPoint, error)
	startImpl func(*WinEventProbe, chan struct{}) error
	shutdownImpl func(*WinEventProbe, context.Context) error
)

// Collect gathers Windows Event Log entries and returns them as DataPoints
func (p *WinEventProbe) Collect() ([]data_store.DataPoint, error) {
	return collectImpl(p)
}

// OnStart initializes the WinEventProbe
func (p *WinEventProbe) OnStart(quitChannel chan struct{}) error {
	return startImpl(p, quitChannel)
}

// OnShutdown cleans up resources when the WinEventProbe is stopped
func (p *WinEventProbe) OnShutdown(ctx context.Context) error {
	return shutdownImpl(p, ctx)
}

// processEvent converts a Windows Event to a DataPoint
func (p *WinEventProbe) processEvent(channel, provider string, id int, level string, message string, timestamp time.Time) data_store.DataPoint {
	eventTags := []tags.Tag{
		{Key: "channel", Value: channel, Private: false},
		{Key: "provider", Value: provider, Private: false},
		{Key: "event_id", Value: fmt.Sprintf("%d", id), Private: false},
		{Key: "level", Value: level, Private: false},
		{Key: "message", Value: message, Private: false},
	}

	p.logger.Debug().
		Time("timestamp", timestamp).
		Str("channel", channel).
		Str("provider", provider).
		Int("id", id).
		Str("level", level).
		Msg("Collected Windows Event Log entry")

	return data_store.DataPoint{
		Name:      "winevents_event",
		Timestamp: timestamp,
		Value:     1.0, // Event count is always 1
		Tags:      eventTags,
	}
}

// String returns a string representation of the WinEventProbe
func (p *WinEventProbe) String() string {
	return fmt.Sprintf("WinEventProbe{channels=%v, eventIDs=%v, levels=%v}", 
		p.config.Channels, p.config.EventIDs, p.config.Levels)
}