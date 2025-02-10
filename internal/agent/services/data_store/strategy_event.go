// senhub-agent/internal/agent/services/data_store/strategy_event.go
package data_store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	eventFormatter "senhub-agent.go/internal/agent/formats/event"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	eventtypes "senhub-agent.go/internal/agent/types/event"
)

const (
	DefaultQueueSize    = 1000
	DefaultSyncInterval = 30 * time.Second
	MaxMessageSize      = 1024 * 1024 // 1MB
)

type EventSyncStrategyParams struct {
	ServerURL     string // URL de configuration
	ServerURLFull string // URL interne avec "/event/insert"
	QueueSize     int
	SyncInterval  time.Duration
}

type EventSyncStrategy struct {
	buffer      chan eventtypes.EventDataPoint
	config      EventSyncStrategyParams
	server      server.Server
	logger      *logger.Logger
	ticker      *time.Ticker
	tickerOnce  sync.Once
	agentConfig configuration.AgentConfiguration
	formatter   *eventFormatter.Formatter
}

func NewEventSyncStrategy(
	agentConfig configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	logger *logger.Logger,
) SyncStrategy {
	localLogger := logger.With().Str("sync_strategy", "EventSyncStrategy").Logger()

	srv := server.NewServer(
		agentConfig.GetAuthenticationKey(),
		storageConfig["server_url"].(string),
		logger,
	)

	// Configuration par défaut
	config := EventSyncStrategyParams{
		QueueSize:    DefaultQueueSize,
		SyncInterval: DefaultSyncInterval,
	}

	// Pré-configuration avec les paramètres fournis
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

	return &EventSyncStrategy{
		buffer:      make(chan eventtypes.EventDataPoint, config.QueueSize),
		config:      config,
		server:      srv,
		agentConfig: agentConfig,
		logger:      &localLogger,
		formatter:   eventFormatter.NewFormatter(),
	}
}

func (s *EventSyncStrategy) GetStrategyName() string {
	return "event"
}

func (s *EventSyncStrategy) GetStrategyParams() map[string]interface{} {
	return map[string]interface{}{
		"server_url":    s.config.ServerURL,
		"queue_size":    s.config.QueueSize,
		"sync_interval": s.config.SyncInterval,
	}
}

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

func (s *EventSyncStrategy) AddDataPoints(data []datapoint.DataPoint) error {
	s.logger.Debug().
		Int("datapoints_count", len(data)).
		Msgf("Event strategy receiving datapoints")

	for _, dp := range data {
		s.logger.Debug().
			Any("datapoint", dp).
			Msg("Processing datapoint")
		evt := s.formatter.FormatDataPoint(dp)

		if err := evt.Validate(); err != nil {
			s.logger.Error().
				Err(err).
				Msg("Invalid event data")
			continue
		}

		select {
		case s.buffer <- evt:
			s.logger.Debug().Msg("Event added to buffer successfully")
		default:
			select {
			case <-s.buffer:
				s.buffer <- evt
				s.logger.Warn().Msg("Buffer full, dropped oldest event")
			default:
				s.logger.Warn().Msg("Buffer full, discarding event")
			}
		}
	}
	return nil
}

func (s *EventSyncStrategy) getTagValue(tags []tags.Tag, key string) string {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

func (s *EventSyncStrategy) Start() error {
	s.tickerOnce.Do(func() {
		s.ticker = time.NewTicker(s.config.SyncInterval)
		s.logger.Info().
			Int("sync_interval_seconds", int(s.config.SyncInterval.Seconds())).
			Msgf("Starting event sync strategy")

		go func() {
			s.logger.Info().Msg("Starting event sync loop")
			for range s.ticker.C {
				if err := s.doSync(); err != nil {
					s.logger.Error().
						Err(err).
						Msg("Error syncing events")
				}
			}
		}()
	})
	return nil
}

func (s *EventSyncStrategy) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down event sync strategy")
	if s.ticker != nil {
		s.ticker.Stop()
	}
	return s.doSync()
}

func (s *EventSyncStrategy) doSync() error {
	s.logger.Debug().
		Int("buffer_size", len(s.buffer)).
		Int("buffer_capacity", cap(s.buffer)).
		Msg("Event sync - starting sync")
	if len(s.buffer) == 0 {
		return nil
	}

	var events []eventtypes.EventDataPoint
	bufferSize := len(s.buffer)
	s.logger.Debug().
		Int("buffer_size", bufferSize).
		Msg("Event sync - processing events")

	// Vidons le buffer
	for i := 0; i < bufferSize; i++ {
		event := <-s.buffer
		events = append(events, event)
	}

	if err := s.sendEvents(events); err != nil {
		// Remettons les événements dans le buffer
		for _, event := range events {
			s.buffer <- event
		}
		return fmt.Errorf("Error sending events: %w", err)
	}

	s.logger.Debug().
		Int("events_count", len(events)).
		Msg("Event sync - events sent successfully")
	return nil
}

func (s *EventSyncStrategy) chunkEvents(events []eventtypes.EventDataPoint, maxSize int) [][]eventtypes.EventDataPoint {
	var chunks [][]eventtypes.EventDataPoint
	var currentChunk []eventtypes.EventDataPoint
	currentSize := 0

	for _, event := range events {
		eventJson, _ := json.Marshal(event)
		eventSize := len(eventJson)

		if currentSize+eventSize > maxSize {
			chunks = append(chunks, currentChunk)
			currentChunk = []eventtypes.EventDataPoint{event}
			currentSize = eventSize
		} else {
			currentChunk = append(currentChunk, event)
			currentSize += eventSize
		}
	}

	if len(currentChunk) > 0 {
		chunks = append(chunks, currentChunk)
	}

	return chunks
}

func (s *EventSyncStrategy) sendEvents(events []eventtypes.EventDataPoint) error {
	// Convertir en stream JSON
	var streamBody strings.Builder
	for _, event := range events {
		eventJson, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("error marshaling event: %w", err)
		}
		streamBody.Write(eventJson)
		streamBody.WriteString("\n")
	}

	// Afficher le contenu envoyé
	s.logger.Debug().
		Str("events", streamBody.String()).
		Msg("Sending events")

	response, err := s.server.PostStream("/event/insert", streamBody.String())
	if err != nil {
		return fmt.Errorf("error sending events: %w", err)
	}
	defer response.Body.Close()

	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}

	s.logger.Debug().
		Int("response_status_code", response.StatusCode).
		Str("response_status", response.Status).
		Str("response_body", string(respBody)).
		Msg("Event sync - response received")

	if response.StatusCode != http.StatusAccepted { // Changé en 202 Accepted
		return fmt.Errorf("unexpected status code: %d - body: %s", response.StatusCode, string(respBody))
	}

	return nil
}
