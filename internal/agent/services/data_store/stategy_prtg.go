package data_store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ybbus/httpretry"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// Value of the tag with this name will be used as PRTG metric id.
// The placeholder [name] will be replaced by the metric name.
var PrtgTagNaame = "prtg_metric_id"

func CreatePrtgMetricIdTag(metricId string) tags.Tag {
	return tags.Tag{
		Key:     PrtgTagNaame,
		Value:   metricId,
		Private: true,
	}
}

type SyncStrategyPrtg struct {
	/** Store all datapoints */
	buffer Buffer
	http   *http.Client

	agentConfig   configuration.AgentConfiguration
	storageConfig configuration.StorageConfigParams
	logger        *logger.Logger
	ticker        *time.Ticker
	tickerOnce    sync.Once
}

func NewSyncStrategyPrtg(
	agentConfig configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	logger *logger.Logger,
) SyncStrategy {
	localLogger := logger.With().Str("sync_strategy", "SyncStrategyPrtg").Logger()
	http := httpretry.NewDefaultClient(
		// retry up to 3 times
		httpretry.WithMaxRetryCount(3),
	)

	return &SyncStrategyPrtg{
		buffer:        NewBuffer(),
		http:          http,
		storageConfig: storageConfig,
		agentConfig:   agentConfig,
		logger:        &localLogger,
	}
}

func (s *SyncStrategyPrtg) GetStrategyName() string {
	return "prtg"
}

func (s *SyncStrategyPrtg) AddDataPoints(data []DataPoint) error {
	s.buffer.Append(data)
	return nil
}

func (s *SyncStrategyPrtg) ValidateConfigParams(params configuration.StorageConfigParams) error {
	if _, ok := params["data_retention_period"]; !ok {
		return fmt.Errorf("data_retention_period is required")
	}
	if _, ok := params["server_url"]; !ok {
		return fmt.Errorf("server_url is required")
	}
	return nil
}

func (s *SyncStrategyPrtg) Start() error {
	s.tickerOnce.Do(func() { // Ensure the ticker only starts once
		s.logger.Info().Msg("Starting sync strategy")
		ticker := time.NewTicker(5 * time.Second)

		go func() {
			for {
				select {
				case <-ticker.C:
					err := s.doSync()
					if err != nil {
						s.logger.Error().Err(err).Msg("error synchronizing data")
					}
				}
			}
		}()
	})

	return nil
}

func (s *SyncStrategyPrtg) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down sync strategy")
	if s.ticker != nil {
		s.ticker.Stop()
	}

	return s.doSync()
}

// filter an array of T using a test function
func filter[T any](ss []T, test func(T) bool) (ret []T) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return ret
}

func metricId(p DataPoint) string {
	// Find a tag named "metric_id"
	for _, t := range p.Tags {
		if t.Key == "prtg_metric_id" {
			return strings.Replace(t.Value, "[name]", p.Name, -1)
		}
	}

	// Fallback to the name
	return p.Name
}

func (s *SyncStrategyPrtg) doSync() error {
	data := s.buffer.Sync()

	if len(data) == 0 {
		return nil
	}

	// Build a map of the last timestamp per metric id
	lastTimestampPerMetricId := make(map[string]time.Time)
	for _, p := range data {
		metricId := metricId(p)
		if lastTimestamp, ok := lastTimestampPerMetricId[metricId]; ok {
			if p.Timestamp.After(lastTimestamp) {
				lastTimestampPerMetricId[metricId] = p.Timestamp
			}
		} else {
			lastTimestampPerMetricId[metricId] = p.Timestamp
		}
	}
	// Only keep last data point within data retention period
	retention, ok := s.storageConfig["data_retention_period"]
	if !ok {
		return fmt.Errorf("data_retention_period is required")
	}
	retentionDuration, err := time.ParseDuration(retention.(string))
	if err != nil {
		return err
	}
	retentionStart := time.Now().Add(-retentionDuration)

	data = filter(data, func(p DataPoint) bool {
		if !p.Timestamp.After(retentionStart) {
			return false
		}

		metricId := metricId(p)
		if lastTimestamp, ok := lastTimestampPerMetricId[metricId]; ok {
			return p.Timestamp.Equal(lastTimestamp)
		}
		return false
	})

	s.logger.Debug().Any("data", data).Msg("synchronizing data")
	if err := s.doSyncData(data); err != nil {
		s.logger.Error().Err(err).Msg("error synchronizing data")
		s.buffer.AbortSync(data)
		return err
	}

	return nil
}

type PrtgResult struct {
	Channel string  `json:"channel"`
	Value   float32 `json:"value"`
}

type PrtgData struct {
	Prtg struct {
		Result []PrtgResult `json:"result"`
	} `json:"prtg"`
}

func (s *SyncStrategyPrtg) doSyncData(data []DataPoint) error {
	// Transform data points to PRTG format
	jsonData := PrtgData{}
	for _, p := range data {
		jsonData.Prtg.Result = append(jsonData.Prtg.Result, PrtgResult{
			metricId(p),
			p.Value,
		})
	}

	// Send data to PRTG
	requestBody, err := json.Marshal(jsonData)
	if err != nil {
		s.logger.Error().Err(err).Msg("error encoding data.")
		return err
	}
	url, ok := s.storageConfig["server_url"]
	if !ok {
		return fmt.Errorf("server_url is required")
	}
	req, err := s.http.Post(url.(string), "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return err
	}
	if req.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d\n%v", req.StatusCode, req.Body)
	}

	return nil
}
