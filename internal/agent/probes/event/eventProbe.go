// Package event provides an HTTP-based event probe for the agent.
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

// DefaultPort is the default port for the HTTP server.
const DefaultPort = 8080

// DefaultSyncInterval is the default interval for synchronization.
const DefaultSyncInterval = 30 * time.Second

// MinPort is the minimum valid port number.
const MinPort = 1

// MaxPort is the maximum valid port number.
const MaxPort = 65535

// MaxFields is the maximum number of fields allowed in an event.
const MaxFields = 20

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
	Port int
}

// EventProbe is the main struct for the EventProbe.
type EventProbe struct {
	rawConfig map[string]interface{}
	config    EventProbeConfig
	logger    *logger.Logger
	server    *http.Server
	callback  func([]data_store.DataPoint) error
}

// SetCallback sets the callback function for the EventProbe.
func (p *EventProbe) SetCallback(callback func([]data_store.DataPoint) error) {
	p.callback = callback
}

// NewEventProbe creates a new instance of EventProbe.
func NewEventProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	parsedConfig, err := parseEventProbeConfig(config)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[DEBUG] Creating new Event probe with config: %+v\n", parsedConfig)
	return &EventProbe{
		rawConfig: config,
		config:    parsedConfig,
		logger:    logger,
	}, nil
}

// parseEventProbeConfig parses the configuration for the EventProbe.
func parseEventProbeConfig(config map[string]interface{}) (EventProbeConfig, error) {
	errs := []error{}
	var port int = DefaultPort

	if portVal, ok := config["port"].(float64); ok {
		port = int(portVal)
		if port < MinPort || port > MaxPort {
			errs = append(errs, fmt.Errorf("port must be between %d and %d", MinPort, MaxPort))
		}
	}

	if len(errs) > 0 {
		return EventProbeConfig{}, fmt.Errorf("error parsing config: %v", errs)
	}

	return EventProbeConfig{
		Port: port,
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
	fmt.Printf("[INFO] Starting Event probe on port %d\n", p.config.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/event", p.handleEvent)

	p.server = &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", p.config.Port),
		Handler: mux,
	}

	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[ERROR] Failed to start HTTP server: %v\n", err)
		}
	}()

	fmt.Printf("[INFO] HTTP server started successfully\n")
	return nil
}

// OnShutdown stops the EventProbe.
func (p *EventProbe) OnShutdown(ctx context.Context) error {
	if p.server != nil {
		fmt.Printf("[INFO] Stopping Event probe\n")
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
		fmt.Printf("[WARNING] Callback is not set\n")
		return
	}

	if err := p.callback([]data_store.DataPoint{dataPoint}); err != nil {
		fmt.Printf("[ERROR] Failed to send DataPoint to DataStore: %v\n", err)
		http.Error(w, "Failed to process event", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Event processed successfully")
}

// validateEvent validates the incoming event.
func validateEvent(event map[string]interface{}) error {
	requiredFields := []string{"timestamp", "host", "message", "severity"}
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
	} else {
		return fmt.Errorf("timestamp must be a string")
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

	eventTags := []tags.Tag{}
	for key, value := range event {
		if key != "timestamp" {
			eventTags = append(eventTags, tags.Tag{Key: key, Value: fmt.Sprintf("%v", value), Private: false})
		}
	}

	fmt.Printf("[DEBUG] Received Event - timestamp: %v, tags: %v\n", timestamp, eventTags)

	return data_store.DataPoint{
		Name:      "event_event",
		Timestamp: timestamp,
		Value:     1.0, // You can adjust this based on your needs
		Tags:      eventTags,
	}
}

// String returns a string representation of the EventProbe.
func (p *EventProbe) String() string {
	return fmt.Sprintf("EventProbe{port=%d}", p.config.Port)
}
