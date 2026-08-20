package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
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

// EventProbeConfig holds the configuration for the EventProbe.
type EventProbeConfig struct {
	Address  string
	Port     int
	Protocol string
}

// EventProbe is the main struct for the EventProbe.
type EventProbe struct {
	*types.BaseProbe
	rawConfig    map[string]interface{}
	config       EventProbeConfig
	moduleLogger *logger.ModuleLogger
	server       *http.Server
	callback     func([]data_store.DataPoint) error
	// serving reports whether the HTTP listener is up. serveErr holds
	// the reason it is not. ListenAndServe runs on its own goroutine
	// and its error used to be logged and dropped, so a port already in
	// use killed the listener at boot while the probe kept reporting
	// healthy for the life of the agent (#289).
	serving  atomic.Bool
	serveErr atomic.Pointer[string]
}

// ListenerHealth implements types.ListenerProbe: this probe receives
// events over HTTP, so its health is whether that listener is serving,
// not whether its no-op Collect returned.
func (p *EventProbe) ListenerHealth() error {
	if msg := p.serveErr.Load(); msg != nil {
		return errors.New(*msg)
	}
	if !p.serving.Load() {
		return errors.New("HTTP listener is not running")
	}
	return nil
}

// eventProbeSeverity maps an accepted severity name to its OTel
// SeverityNumber. The ladder lives in agentstate, once: this used to be
// a third hand-maintained copy of the same eight rungs, alongside a
// separate validation set that had to be kept in step with it (#294).
// An unaccepted name cannot reach here — the payload is rejected at
// validation — so the miss returns Unspecified rather than guessing.
func eventProbeSeverity(name string) agentstate.LogSeverity {
	sev, _ := agentstate.EventProbeSeverityToOTel(name)
	return sev
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
		BaseProbe:    &types.BaseProbe{},
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

	if v, ok := types.IntParam(config, "port"); ok {
		port = v
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
		return EventProbeConfig{}, fmt.Errorf("error parsing config: %w", errors.Join(errs...))
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

// Note: GetName() is now inherited from BaseProbe and will return the unique
// probe name from configuration (e.g., "event", "event2") instead of the
// hardcoded type. This enables proper discriminant tagging for multiple instances.

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
		Addr:         fmt.Sprintf("%s:%d", p.config.Address, p.config.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	p.serveErr.Store(nil)
	p.serving.Store(true)
	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			p.moduleLogger.Error().Err(err).Msg("Failed to start HTTP server")
			msg := err.Error()
			p.serveErr.Store(&msg)
		}
		p.serving.Store(false)
	}()

	p.moduleLogger.Info().Msg("Event probe started successfully")
	return nil
}

// OnShutdown stops the EventProbe.
func (p *EventProbe) OnShutdown(ctx context.Context) error {
	p.serving.Store(false)
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

	// The event is a log payload — publish it once on the agent log bus
	// (#294 step 1b). Its structured fields ride LogRecord.Fields so the
	// event strategy rebuilds /event/insert byte-identically and the OTLP
	// strategy can ship it. The former metric DataPoint → data_store →
	// event strategy path was a duplicate of the same event and was removed.
	p.publishLog(event, parseEventTimestamp(event))

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Event processed successfully")
}

// publishLog converts a validated incoming HTTP event into an OTel-shaped
// log record and pushes it onto the agent log channel. Validation has
// already guaranteed the required fields are present and well-typed,
// so this method only handles the safe-extract + map.
func (p *EventProbe) publishLog(event map[string]interface{}, timestamp time.Time) {
	severityStr, _ := event["severity"].(string)
	body, _ := event["message"].(string)
	host, _ := event["host"].(string)

	attrs := map[string]string{
		"event.host":     host,
		"event.severity": severityStr,
	}
	for k, v := range event {
		switch k {
		case "host", "message", "severity", "timestamp":
			continue
		}
		// Stringify all extras under a senhub.event.* namespace so
		// the receiver can distinguish probe-supplied attributes
		// from the standard ones above.
		attrs["senhub.event."+k] = fmt.Sprintf("%v", v)
	}

	agentstate.PublishLog(agentstate.LogRecord{
		TargetStrategies: p.LogTargets(),
		Timestamp:        timestamp,
		Severity:         eventProbeSeverity(severityStr),
		SeverityText:     severityStr,
		Body:             body,
		Attributes:       attrs,
		// Fields carries the raw event map so the /event/insert converter
		// (FromEventLog) rebuilds the exact legacy payload, structure
		// included — the flat Attributes above cannot hold arrays/objects
		// (#294 step 1b).
		Fields:            event,
		ProducerProbeName: p.GetName(),
		ProducerProbeType: "event",
	})
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
			return fmt.Errorf("invalid timestamp format, must be ISO8601: %w", err)
		}
	}

	if host, ok := event["host"].(string); !ok || host == "" {
		return fmt.Errorf("host must be a non-empty string")
	}

	if message, ok := event["message"].(string); !ok || message == "" {
		return fmt.Errorf("message must be a non-empty string")
	}

	if severity, ok := event["severity"].(string); ok {
		if _, valid := agentstate.EventProbeSeverityToOTel(severity); !valid {
			return fmt.Errorf("invalid severity value: %s (accepted: %s)",
				severity, strings.Join(agentstate.EventProbeSeverityNames(), ", "))
		}
	} else {
		return fmt.Errorf("severity must be a string")
	}

	return nil
}

// processEvent processes the incoming event and converts it to a DataPoint.
// parseEventTimestamp reads the optional RFC3339 `timestamp` field from an
// incoming event, falling back to now. The event's structured payload is
// carried verbatim on LogRecord.Fields; the /event/insert shape is rebuilt
// on the consumer side by the formatter (EventMapToDataPoint), so the probe
// no longer builds a DataPoint itself (#294 step 1b).
func parseEventTimestamp(event map[string]interface{}) time.Time {
	if ts, ok := event["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t
		}
	}
	return time.Now()
}

// String returns a string representation of the EventProbe.
func (p *EventProbe) String() string {
	return fmt.Sprintf("EventProbe{address=%s, port=%d, protocol=%s}", p.config.Address, p.config.Port, p.config.Protocol)
}
