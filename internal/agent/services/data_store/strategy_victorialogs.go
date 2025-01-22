// internal/agent/services/data_store/strategy_victorialogs.go
package data_store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

type LokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"` // [timestamp, message]
}

type LokiRequest struct {
	Streams []LokiStream `json:"streams"`
}

type VictoriaLogsStrategy struct {
	buffer        Buffer
	storageConfig configuration.StorageConfigParams
	logger        *logger.Logger
	client        *http.Client
	ticker        *time.Ticker
	tickerOnce    sync.Once
}

func NewVictoriaLogsStrategy(
	_ configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	logger *logger.Logger,
) SyncStrategy {
	localLogger := logger.With().Str("sync_strategy", "VictoriaLogsStrategy").Logger()
	return &VictoriaLogsStrategy{
		buffer:        NewBuffer(),
		storageConfig: storageConfig,
		logger:        &localLogger,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *VictoriaLogsStrategy) GetStrategyName() string {
	return "victorialogs"
}

func (s *VictoriaLogsStrategy) GetStrategyParams() map[string]interface{} {
	return s.storageConfig
}

func (s *VictoriaLogsStrategy) ValidateConfigParams(params configuration.StorageConfigParams) error {
	if url, ok := params["url"].(string); !ok || url == "" {
		return fmt.Errorf("url parameter is required")
	}
	return nil
}

func (s *VictoriaLogsStrategy) AddDataPoints(data []DataPoint) error {
	s.buffer.Append(data)
	return nil
}

func (s *VictoriaLogsStrategy) Start() error {
	s.tickerOnce.Do(func() {
		s.logger.Info().Msg("Starting VictoriaLogs sync strategy")
		s.ticker = time.NewTicker(5 * time.Second)
		go func() {
			for {
				select {
				case <-s.ticker.C:
					if err := s.doSync(); err != nil {
						s.logger.Error().Err(err).Msg("error synchronizing data")
					}
				}
			}
		}()
	})
	return nil
}

func (s *VictoriaLogsStrategy) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down VictoriaLogs sync strategy")
	if s.ticker != nil {
		s.ticker.Stop()
	}
	return s.doSync()
}

func (s *VictoriaLogsStrategy) doSync() error {
	data := s.buffer.Sync()
	if len(data) == 0 {
		return nil
	}

	// Construire une seule stream avec tous les logs
	request := LokiRequest{
		Streams: []LokiStream{
			{
				Stream: map[string]string{},
				Values: make([][2]string, 0, len(data)),
			},
		},
	}

	hasValidLogs := false
	for _, dp := range data {
		// Extraire les labels et le message des tags
		var message string
		stream := make(map[string]string)

		for _, tag := range dp.Tags {
			if !tag.Private {
				switch tag.Key {
				case "message":
					message = fmt.Sprintf("%v", tag.Value)
				case "facility", "severity", "hostname", "app_name":
					stream[tag.Key] = fmt.Sprintf("%v", tag.Value)
				}
			}
		}

		// Ignorer ce log s'il n'a pas de message
		if message == "" {
			continue
		}

		// Ajouter le log à la stream
		timestamp := fmt.Sprintf("%d", dp.Timestamp.UnixNano())
		request.Streams[0].Values = append(request.Streams[0].Values, [2]string{timestamp, message})
		request.Streams[0].Stream = stream
		hasValidLogs = true
	}

	// Ne rien envoyer si aucun log valide
	if !hasValidLogs {
		return nil
	}

	fmt.Printf("\nSending batch to Loki (%d logs):\n", len(request.Streams[0].Values))
	for _, value := range request.Streams[0].Values {
		fmt.Printf("  [%s] %s (labels: %v)\n",
			value[0],                  // timestamp
			value[1],                  // message
			request.Streams[0].Stream) // labels
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		s.buffer.AbortSync(data)
		return fmt.Errorf("error marshaling data: %w", err)
	}

	// Log pour debug
	fmt.Printf("Sending data to Loki: %s\n", string(jsonData))

	req, err := http.NewRequest("POST", s.storageConfig["url"].(string), bytes.NewBuffer(jsonData))
	if err != nil {
		s.buffer.AbortSync(data)
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.buffer.AbortSync(data)
		return fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		s.buffer.AbortSync(data)
		return fmt.Errorf("received error status code %d from Loki: %s", resp.StatusCode, string(body))
	}

	return nil
}
