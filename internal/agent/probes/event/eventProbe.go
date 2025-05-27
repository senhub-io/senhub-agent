package event

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// Default values
const (
	DefaultAddress      = "127.0.0.1"
	DefaultPort         = 5656
	DefaultProtocol     = "tcp"
	DefaultSyncInterval = 30 * time.Second
	MinPort             = 1
	MaxPort             = 65535
	MaxFields           = 20
)

// validSeverities is a map of valid severity levels.
var validSeverities = map[string]struct{}{
	"EMERG":   {},
	"ALERT":   {},
	"CRIT":    {},
	"ERR":     {},
	"WARNING": {},
	"NOTICE":  {},
	"INFO":    {},
	"DEBUG":   {},
}

// EventProbeConfig holds the configuration for the EventProbe.
type EventProbeConfig struct {
	Address  string
	Port     int
	Protocol string
}

// EventProbe is the main struct for the EventProbe.
type EventProbe struct {
	rawConfig    map[string]interface{}
	config       EventProbeConfig
	moduleLogger *logger.ModuleLogger
	server       *http.Server
	callback     func([]data_store.DataPoint) error
}

// SetCallback sets the callback function for the EventProbe.
func (p *EventProbe) SetCallback(callback func([]data_store.DataPoint) error) {
	p.callback = callback
}

// NewEventProbe creates a new instance of EventProbe.
func NewEventProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	parsedConfig, err := parseEventProbeConfig(config)
	if err != nil {
		return nil, err
	}
	
	// Create module-specific logger for event probe
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.event")

	moduleLogger.Debug().
		Any("config", parsedConfig).
		Msg("Creating new Event probe")
		
	return &EventProbe{
		rawConfig:    config,
		config:       parsedConfig,
		moduleLogger: moduleLogger,
	}, nil
}

// parseEventProbeConfig parses the configuration for the EventProbe.
func parseEventProbeConfig(config map[string]interface{}) (EventProbeConfig, error) {
	errs := []error{}
	var port int = DefaultPort
	var protocol string = DefaultProtocol
	var address string = DefaultAddress

	if portVal, ok := config["port"].(float64); ok {
		port = int(portVal)
		if port < MinPort || port > MaxPort {
			errs = append(errs, fmt.Errorf("port must be between %d and %d", MinPort, MaxPort))
		}
	}

	if protocolVal, ok := config["protocol"].(string); ok {
		protocol = protocolVal
		if protocol != "tcp" && protocol != "udp" {
			errs = append(errs, fmt.Errorf("protocol must be 'tcp' or 'udp'"))
		}
	}

	if addrVal, ok := config["address"].(string); ok {
		address = addrVal
	}

	if len(errs) > 0 {
		return EventProbeConfig{}, fmt.Errorf("error parsing config: %v", errs)
	}

	return EventProbeConfig{
		Address:  address,
		Port:     port,
		Protocol: protocol,
	}, nil
}

// GetTargetStrategies returns the target strategies for the EventProbe.
func (p *EventProbe) GetTargetStrategies() []string {
	return []string{"event"}
}

// GetName returns the name of the EventProbe.
func (p *EventProbe) GetName() string {
	return "event"
}

// ShouldStart indicates whether the EventProbe should start.
func (p *EventProbe) ShouldStart() bool {
	return true
}

// GetInterval returns the interval for the EventProbe.
func (p *EventProbe) GetInterval() time.Duration {
	return DefaultSyncInterval
}

// Collect is a placeholder method for periodic collection (not used in this probe).
func (p *EventProbe) Collect() ([]data_store.DataPoint, error) {
	return nil, nil // Event-driven, no periodic collection
}

// OnStart starts the EventProbe.
func (p *EventProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Debug().Msg("Starting Event probe")

	mux := http.NewServeMux()
	mux.HandleFunc("/event", p.handleEvent)

	p.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", p.config.Address, p.config.Port),
		Handler: mux,
	}

	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			p.moduleLogger.Error().Err(err).Msg("Failed to start HTTP server")
		}
	}()

	p.moduleLogger.Info().Msg("Event probe started successfully")
	return nil
}

// OnShutdown stops the EventProbe.
func (p *EventProbe) OnShutdown(ctx context.Context) error {
	if p.server != nil {
		p.moduleLogger.Info().Msg("Stopping Event probe")
		return p.server.Shutdown(ctx)
	}
	return nil
}

// handleEvent handles incoming HTTP events.
func (p *EventProbe) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var event map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := validateEvent(event); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dataPoint := p.processEvent(event)
	if p.callback == nil {
		p.moduleLogger.Warn().Msg("Callback is not set")
		return
	}

	if err := p.callback([]data_store.DataPoint{dataPoint}); err != nil {
		p.moduleLogger.Error().Err(err).Msg("Failed to send DataPoint to DataStore")
		http.Error(w, "Failed to process event", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Event processed successfully")
}

// validateEvent validates the incoming event.
func validateEvent(event map[string]interface{}) error {
	requiredFields := []string{"host", "message", "severity"}
	for _, field := range requiredFields {
		if _, ok := event[field]; !ok {
			return fmt.Errorf("missing required field: %s", field)
		}
	}

	if len(event) > MaxFields {
		return fmt.Errorf("too many fields, maximum allowed is %d", MaxFields)
	}

	if ts, ok := event["timestamp"].(string); ok {
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			return fmt.Errorf("invalid timestamp format, must be ISO8601: %v", err)
		}
	}

	if host, ok := event["host"].(string); !ok || host == "" {
		return fmt.Errorf("host must be a non-empty string")
	}

	if message, ok := event["message"].(string); !ok || message == "" {
		return fmt.Errorf("message must be a non-empty string")
	}

	if severity, ok := event["severity"].(string); ok {
		if _, valid := validSeverities[severity]; !valid {
			return fmt.Errorf("invalid severity value: %s", severity)
		}
	} else {
		return fmt.Errorf("severity must be a string")
	}

	return nil
}

// processEvent processes the incoming event and converts it to a DataPoint.
func (p *EventProbe) processEvent(event map[string]interface{}) data_store.DataPoint {
	timestamp := time.Now()
	if ts, ok := event["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			timestamp = t
		}
	}

	// Create two sets of tags:
	// 1. Standard string tags for required fields and simple values
	// 2. A special JSON metadata field that preserves complex types like arrays
	eventTags := []tags.Tag{}
	complexValues := make(map[string]interface{})
	
	for key, value := range event {
		if key == "timestamp" {
			continue
		}
		
		// Store all values as strings in regular tags for backward compatibility
		eventTags = append(eventTags, tags.Tag{Key: key, Value: fmt.Sprintf("%v", value), Private: false})
		
		// Also store complex values in their original form
		switch v := value.(type) {
		case []interface{}, map[string]interface{}:
			// These are complex types that should be preserved
			complexValues[key] = v
		}
	}
	
	// If we have complex values, serialize them as JSON and add as a special tag
	if len(complexValues) > 0 {
		complexJSON, err := json.Marshal(complexValues)
		if err == nil {
			eventTags = append(eventTags, tags.Tag{
				Key:     "_complex_values",
				Value:   string(complexJSON),
				Private: false,
			})
		} else {
			p.moduleLogger.Error().Err(err).Msg("Failed to marshal complex values")
		}
	}

	p.moduleLogger.Debug().
		Time("timestamp", timestamp).
		Any("tags", eventTags).
		Msg("Received Event")

	return data_store.DataPoint{
		Name:      "event_event",
		Timestamp: timestamp,
		Value:     1.0, // You can adjust this based on your needs
		Tags:      eventTags,
	}
}

// String returns a string representation of the EventProbe.
func (p *EventProbe) String() string {
	return fmt.Sprintf("EventProbe{address=%s, port=%d, protocol=%s}", p.config.Address, p.config.Port, p.config.Protocol)
}
