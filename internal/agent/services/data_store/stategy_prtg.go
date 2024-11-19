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
	"senhub-agent.go/internal/agent/validators"
)

var (
	// Value of the tag with this name will be used as PRTG metric id.
	// The placeholder [name] will be replaced by the metric name.
	PrtgTagName = "prtg_metric_id"
	// Default interval for synchronization
	DEFAULT_INTERVAL = 5 * time.Second
	// Default data retention period
	DEFAULT_RETENTION_PERIOD = 2 * time.Minute
)

type SyncStrategyPrtgParams struct {
	Interval        time.Duration
	RetentionPeriod time.Duration
	ServerUrl       string
}

func CreatePrtgMetricIdTag(metricId string) tags.Tag {
	return tags.Tag{
		Key:     PrtgTagName,
		Value:   metricId,
		Private: true,
	}
}

type SyncStrategyPrtg struct {
	/** Store all datapoints */
	buffer Buffer
	http   *http.Client

	agentConfig configuration.AgentConfiguration
	rawConfig   configuration.StorageConfigParams
	config      SyncStrategyPrtgParams
	logger      *logger.Logger
	ticker      *time.Ticker
	tickerOnce  sync.Once
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
		buffer:      NewBuffer(),
		http:        http,
		rawConfig:   storageConfig,
		agentConfig: agentConfig,
		logger:      &localLogger,
	}
}

func ParseSyncStrategyPrtgParams(config configuration.StorageConfigParams) (SyncStrategyPrtgParams, error) {
	errs := []error{}
	params := SyncStrategyPrtgParams{
		Interval:        DEFAULT_INTERVAL,
		RetentionPeriod: DEFAULT_RETENTION_PERIOD,
		ServerUrl:       "",
	}

	if intervalStr, ok := config["interval"]; ok {
		if !validators.IsDuration(intervalStr) {
			errs = append(errs, fmt.Errorf("interval must be a valid duration"))
		} else {
			parsedInterval, err := time.ParseDuration(intervalStr.(string))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing interval: %w", err))
			} else {
				params.Interval = parsedInterval
			}
		}
	}

	if intervalStr, ok := config["data_retention_period"]; ok {
		if !validators.IsDuration(intervalStr) {
			errs = append(errs, fmt.Errorf("data_retention_period must be a valid duration"))
		} else {
			parsedRetentionPeriod, err := time.ParseDuration(intervalStr.(string))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing data_retention_period: %w", err))
			} else {
				params.RetentionPeriod = parsedRetentionPeriod
			}
		}
	}

	url, ok := config["url"].(string)
	if !ok || url == "" {
		errs = append(errs, fmt.Errorf("url parameter is required"))
	} else if !validators.IsURL(url) {
		errs = append(errs, fmt.Errorf("url must be a valid URL"))
	} else {
		params.ServerUrl = url
	}

	if len(errs) > 0 {
		return params, fmt.Errorf("error parsing config: %v", errs)
	}

	return params, nil
}

func (s *SyncStrategyPrtg) GetStrategyName() string {
	return "prtg"
}

func (s *SyncStrategyPrtg) AddDataPoints(data []DataPoint) error {
	s.buffer.Append(data)
	return nil
}

func (s *SyncStrategyPrtg) ValidateConfigParams(params configuration.StorageConfigParams) error {
	config, err := ParseSyncStrategyPrtgParams(params)
	if err != nil {
		return err
	}

	s.config = config
	return nil
}

func (s *SyncStrategyPrtg) Start() error {
	s.tickerOnce.Do(func() { // Ensure the ticker only starts once
		s.logger.Info().Msg("Starting sync strategy")
		interval := s.config.Interval
		ticker := time.NewTicker(interval)

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
	retentionPeriod := s.config.RetentionPeriod
	retentionStart := time.Now().Add(-retentionPeriod)

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

	// Some probes might not read nez value until the next sync, so valid data
	// points are restored in the buffer.
	// This happens wether the sync si successful or not.
	s.buffer.AbortSync(data)
	if err := s.doSyncData(data); err != nil {
		s.logger.Error().Err(err).Msg("error synchronizing data")
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
	req, err := s.http.Post(
		s.config.ServerUrl,
		"application/json",
		bytes.NewBuffer(requestBody),
	)
	if err != nil {
		return err
	}
	if req.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d\n%v", req.StatusCode, req.Body)
	}

	return nil
}
