package event

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/avast/retry-go/v4"

	eventFormatter "senhub-agent.go/internal/agent/formats/event"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
	"senhub-agent.go/internal/agent/types/datapoint"
	eventtypes "senhub-agent.go/internal/agent/types/event"
)

const (
	// DefaultQueueSize is the default size of the event buffer
	DefaultQueueSize = 1000
	// DefaultSyncInterval is the default interval between syncs
	DefaultSyncInterval = 30 * time.Second
	// MaxMessageSize is the maximum size of a single message in bytes
	MaxMessageSize = 1024 * 1024 // 1MB
	// DefaultChunkSize is the default number of events per chunk
	DefaultChunkSize = 100
	// DefaultRetryAttempts is the number of retry attempts for failed syncs
	DefaultRetryAttempts = 3
	// DefaultRetryDelay is the delay between retry attempts
	DefaultRetryDelay = time.Second
)

// EventSyncStrategyParams holds the configuration for the event sync strategy
type EventSyncStrategyParams struct {
	ServerURL     string        // Base URL for the server
	ServerURLFull string        // Full URL including the endpoint path
	QueueSize     int           // Size of the event buffer
	SyncInterval  time.Duration // Interval between syncs
}

// EventSyncStrategy implements the SyncStrategy interface for event synchronization
type EventSyncStrategy struct {
	buffer         chan eventtypes.EventDataPoint
	syncInProgress atomic.Bool
	currentSize    atomic.Int64 // Current size of buffered events in bytes
	failedEvents   []eventtypes.EventDataPoint
	mutex          sync.Mutex // Protects failedEvents

	syncTriggerSize  int   // Number of events that triggers a sync
	syncTriggerBytes int64 // Size in bytes that triggers a sync

	config      EventSyncStrategyParams
	server      server.Server
	logger      *logger.ModuleLogger
	ticker      *time.Ticker
	tickerOnce  sync.Once
	agentConfig configuration.AgentConfiguration
	formatter   *eventFormatter.Formatter
}

// NewEventSyncStrategy creates a new instance of EventSyncStrategy
func NewEventSyncStrategy(
	agentConfig configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	baseLogger *logger.Logger,
) interface{} {
	// Create module-specific logger for event strategy
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.event")

	srv := server.NewServer(
		agentConfig.GetAuthenticationKey(),
		storageConfig["server_url"].(string),
		baseLogger,
	)

	// Default configuration
	config := EventSyncStrategyParams{
		QueueSize:    DefaultQueueSize,
		SyncInterval: DefaultSyncInterval,
	}

	// Apply provided configuration
	if url, ok := storageConfig["server_url"].(string); ok {
		config.ServerURL = url
		config.ServerURLFull = url + "/event/insert"
	}
	if size, ok := storageConfig["queue_size"].(int); ok {
		config.QueueSize = size
	}
	if interval, ok := storageConfig["sync_interval"].(string); ok {
		if duration, err := time.ParseDuration(interval); err == nil {
			config.SyncInterval = duration
		}
	}

	strategy := &EventSyncStrategy{
		buffer:           make(chan eventtypes.EventDataPoint, config.QueueSize),
		config:           config,
		server:           srv,
		agentConfig:      agentConfig,
		logger:           moduleLogger,
		formatter:        eventFormatter.NewFormatter(),
		syncTriggerSize:  DefaultChunkSize,
		syncTriggerBytes: MaxMessageSize / 2, // Trigger at 50% of max message size
	}

	// Initialize atomic values
	strategy.currentSize.Store(0)
	strategy.syncInProgress.Store(false)

	return strategy
}

// GetStrategyName returns the name of the strategy
func (s *EventSyncStrategy) GetStrategyName() string {
	return "event"
}

// GetStrategyParams returns the current configuration parameters
func (s *EventSyncStrategy) GetStrategyParams() map[string]interface{} {
	return map[string]interface{}{
		"server_url":    s.config.ServerURL,
		"queue_size":    s.config.QueueSize,
		"sync_interval": s.config.SyncInterval,
	}
}

// ValidateConfigParams validates the provided configuration parameters
func (s *EventSyncStrategy) ValidateConfigParams(params configuration.StorageConfigParams) error {
	config := EventSyncStrategyParams{
		QueueSize:    DefaultQueueSize,
		SyncInterval: DefaultSyncInterval,
	}

	if url, ok := params["server_url"].(string); !ok || url == "" {
		return fmt.Errorf("server_url is required")
	} else {
		config.ServerURL = url
		config.ServerURLFull = url + "/event/insert"
	}

	if size, ok := params["queue_size"].(int); ok {
		config.QueueSize = size
	}

	if interval, ok := params["sync_interval"].(string); ok {
		duration, err := time.ParseDuration(interval)
		if err != nil {
			return fmt.Errorf("invalid sync_interval: %w", err)
		}
		config.SyncInterval = duration
	}

	s.config = config

	// Resize buffer if queue size changed
	if cap(s.buffer) != config.QueueSize {
		newBuffer := make(chan eventtypes.EventDataPoint, config.QueueSize)
		close(s.buffer)
		s.buffer = newBuffer
	}

	return nil
}

// AddDataPoints adds new datapoints to the buffer and triggers sync if needed
func (s *EventSyncStrategy) AddDataPoints(data []datapoint.DataPoint) error {
	for _, dp := range data {
		evt := s.formatter.FormatDataPoint(dp)
		if err := evt.Validate(); err != nil {
			s.logger.Error().Err(err).Msg("Invalid event data")
			continue
		}

		// Calculate event size
		eventJson, err := json.Marshal(evt)
		if err != nil {
			s.logger.Error().Err(err).Msg("Failed to marshal event")
			continue
		}
		eventSize := int64(len(eventJson))

		// Try to add to buffer
		select {
		case s.buffer <- evt:
			newSize := s.currentSize.Add(eventSize)
			s.logger.Debug().Msg("Event added to buffer successfully")

			// Check if we should trigger a sync
			if len(s.buffer) >= s.syncTriggerSize || newSize >= s.syncTriggerBytes {
				s.triggerSync()
			}
		default:
			// Buffer is full, try to make room
			s.logger.Warn().Msg("Buffer full, attempting to make room")
			select {
			case <-s.buffer: // Remove oldest event
				s.buffer <- evt
				s.logger.Warn().Msg("Dropped oldest event to make room")
			default:
				s.logger.Error().Msg("Failed to make room in buffer, event lost")
			}
		}
	}
	return nil
}

// triggerSync initiates an asynchronous sync if none is already in progress
func (s *EventSyncStrategy) triggerSync() {
	if s.syncInProgress.CompareAndSwap(false, true) {
		go func() {
			defer s.syncInProgress.Store(false)
			if err := s.doSync(); err != nil {
				s.logger.Error().
					Err(err).
					Msg("Sync failed, events preserved for retry")
			}
		}()
	}
}

// doSync performs the actual synchronization with chunking and error handling
func (s *EventSyncStrategy) doSync() error {
	var events []eventtypes.EventDataPoint
	var currentBatchSize int64

	// First handle any previously failed events
	s.mutex.Lock()
	if len(s.failedEvents) > 0 {
		s.logger.Info().
			Int("count", len(s.failedEvents)).
			Msg("Processing previously failed events")
		events = append(events, s.failedEvents...)
		s.failedEvents = nil
	}
	s.mutex.Unlock()

	// Collect events up to chunk limits
	for len(events) < s.syncTriggerSize && len(s.buffer) > 0 {
		select {
		case evt := <-s.buffer:
			eventJson, err := json.Marshal(evt)
			if err != nil {
				s.logger.Error().Err(err).Msg("Failed to marshal event during sync")
				continue
			}

			currentBatchSize += int64(len(eventJson))
			if currentBatchSize > s.syncTriggerBytes {
				// Put the event back if it would exceed size limit
				s.buffer <- evt
				break
			}

			events = append(events, evt)
		default:
			break
		}
	}

	if len(events) == 0 {
		return nil
	}

	// Try to send events with retry mechanism
	err := retry.Do(
		func() error {
			return s.sendEvents(events)
		},
		retry.Attempts(DefaultRetryAttempts),
		retry.Delay(DefaultRetryDelay),
		retry.OnRetry(func(n uint, err error) {
			s.logger.Warn().
				Err(err).
				Uint("attempt", n+1).
				Int("events_count", len(events)).
				Msg("Retrying event sync")
		}),
	)

	if err != nil {
		// Preserve failed events for next sync attempt
		s.mutex.Lock()
		s.failedEvents = append(s.failedEvents, events...)
		s.mutex.Unlock()
		return fmt.Errorf("failed to sync events after %d attempts: %w", DefaultRetryAttempts, err)
	}

	// Update metrics after successful send
	s.currentSize.Add(-currentBatchSize)
	s.logger.Info().
		Int("events_sent", len(events)).
		Int64("batch_size_bytes", currentBatchSize).
		Msg("Successfully synced events")

	return nil
}

// sendEvents sends a batch of events to the server
func (s *EventSyncStrategy) sendEvents(events []eventtypes.EventDataPoint) error {
	if len(events) == 0 {
		return nil
	}

	// Marshal all events as a single JSON array
	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("error marshaling events array: %w", err)
	}

	s.logger.Debug().
		Int("event_count", len(events)).
		Int("payload_size", len(eventsJSON)).
		Msg("Sending batch of events")

	response, err := s.server.PostStream("/event/insert", string(eventsJSON))
	if err != nil {
		return fmt.Errorf("error sending events: %w", err)
	}
	defer response.Body.Close()

	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}

	// Accept both 200 OK and 202 Accepted as successful responses
	if response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d - body: %s", response.StatusCode, string(respBody))
	}

	s.logger.Info().
		Int("status_code", response.StatusCode).
		Int("event_count", len(events)).
		Msg("Server confirmed receipt of events")

	return nil
}

// Start initializes and starts the sync strategy
func (s *EventSyncStrategy) Start() error {
	s.tickerOnce.Do(func() {
		s.ticker = time.NewTicker(s.config.SyncInterval)
		s.logger.Info().
			Dur("interval", s.config.SyncInterval).
			Int("queue_size", s.config.QueueSize).
			Msg("Starting event sync strategy")

		go func() {
			for range s.ticker.C {
				s.triggerSync()
			}
		}()
	})
	return nil
}

// Shutdown performs a graceful shutdown of the sync strategy
func (s *EventSyncStrategy) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Initiating graceful shutdown")
	if s.ticker != nil {
		s.ticker.Stop()
	}

	// Wait for ongoing sync to complete
	for s.syncInProgress.Load() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			s.logger.Debug().Msg("Waiting for ongoing sync to complete")
		}
	}

	// Final sync of remaining events
	return s.doSync()
}
