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
	fmt.Printf("[DEBUG] Event strategy receiving %d datapoints\n", len(data))

	for _, dp := range data {
		fmt.Printf("[DEBUG] Processing datapoint: %+v\n", dp)
		evt := s.formatter.FormatDataPoint(dp)

		if err := evt.Validate(); err != nil {
			fmt.Printf("[ERROR] Invalid event data: %v\n", err)
			continue
		}

		select {
		case s.buffer <- evt:
			fmt.Printf("[DEBUG] Event added to buffer successfully\n")
		default:
			select {
			case <-s.buffer:
				s.buffer <- evt
				fmt.Printf("[WARN] Buffer full, dropped oldest event\n")
			default:
				fmt.Printf("[WARN] Buffer full, discarding event\n")
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
		fmt.Printf("[DEBUG] Created ticker with interval %v\n", s.config.SyncInterval)

		go func() {
			fmt.Printf("[DEBUG] Starting event sync loop\n")
			for range s.ticker.C {
				if err := s.doSync(); err != nil {
					fmt.Printf("[ERROR] Sync error: %v\n", err)
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
	fmt.Printf("[DEBUG] Event sync - buffer size: %d/%d\n", len(s.buffer), cap(s.buffer))
	if len(s.buffer) == 0 {
		return nil
	}

	var events []eventtypes.EventDataPoint
	bufferSize := len(s.buffer)
	fmt.Printf("[DEBUG] Event sync - processing %d events\n", bufferSize)

	// Vidons le buffer
	for i := 0; i < bufferSize; i++ {
		event := <-s.buffer
		events = append(events, event)
	}

	if err := s.sendEvents(events); err != nil {
		fmt.Printf("[ERROR] Failed to send events: %v\n", err)
		// Remettons les événements dans le buffer
		for _, event := range events {
			s.buffer <- event
		}
		return err
	}

	fmt.Printf("[DEBUG] Event sync - successfully sent %d events\n", len(events))
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
	fmt.Printf("DEBUG - Sending events: %s\n", streamBody.String())

	response, err := s.server.PostStream("/event/insert", streamBody.String())
	if err != nil {
		return fmt.Errorf("error sending events: %w", err)
	}
	defer response.Body.Close()

	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}

	fmt.Printf("DEBUG - Server response (status=%d): %s\n", response.StatusCode, string(respBody))

	if response.StatusCode != http.StatusAccepted { // Changé en 202 Accepted
		return fmt.Errorf("unexpected status code: %d - body: %s", response.StatusCode, string(respBody))
	}

	return nil
}
