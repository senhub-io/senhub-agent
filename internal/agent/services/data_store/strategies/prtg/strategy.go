package prtg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ybbus/httpretry"
	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/validators"
)

var (
	// Default interval for synchronization
	DEFAULT_PRTG_INTERVAL = 5 * time.Second
	// Default data retention period
	DEFAULT_RETENTION_PERIOD = 2 * time.Minute
)

// defaultPushTimeout bounds every outbound PRTG push (all retries + body
// read) so a hung endpoint cannot block the sync goroutine forever.
const defaultPushTimeout = 30 * time.Second

// Buffer interface for local use to avoid import cycles
type Buffer interface {
	// Append appends data to the buffer
	Append(newData []datapoint.DataPoint) error
	// Flush the buffer data and return the data
	Sync() []datapoint.DataPoint
	// Revert the sync operation
	AbortSync(failedData []datapoint.DataPoint) error
}

// buffer implements Buffer interface
type buffer struct {
	data      *[]datapoint.DataPoint
	mutex     sync.Mutex
	maxPoints int // 0 = unbounded
}

// NewBuffer creates a new buffer instance
func NewBuffer() Buffer {
	return &buffer{
		data:      &[]datapoint.DataPoint{},
		maxPoints: defaultMaxBufferPoints,
	}
}

// defaultMaxBufferPoints mirrors the senhub cloud buffer cap (#267):
// a PRTG endpoint outage must not grow this buffer until OOM. Oldest
// points are dropped first.
const defaultMaxBufferPoints = 100000

// trimToCap drops the OLDEST points past the cap. Callers hold mutex.
func (b *buffer) trimToCap() {
	if b.maxPoints <= 0 || len(*b.data) <= b.maxPoints {
		return
	}
	dropped := len(*b.data) - b.maxPoints
	*b.data = (*b.data)[dropped:]
	agentstate.IncrementPushBufferDropped("prtg", dropped)
}

// Append appends data to the buffer
func (b *buffer) Append(newData []datapoint.DataPoint) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	*b.data = append(*b.data, newData...)
	b.trimToCap()
	return nil
}

// Sync returns all buffered data and clears the buffer
func (b *buffer) Sync() []datapoint.DataPoint {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	data := *b.data
	b.data = &[]datapoint.DataPoint{}
	return data
}

// AbortSync adds failed data back to the buffer
func (b *buffer) AbortSync(failedData []datapoint.DataPoint) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	*b.data = append(failedData, *b.data...)
	b.trimToCap()
	return nil
}

type SyncStrategyPrtgParams struct {
	Interval        time.Duration
	RetentionPeriod time.Duration
	ServerUrl       string
}

type SyncStrategyPrtg struct {
	/** Store all datapoints */
	buffer Buffer
	http   *http.Client

	agentConfig configuration.AgentConfiguration
	rawConfig   configuration.StorageConfigParams
	config      SyncStrategyPrtgParams
	logger      *logger.ModuleLogger
	scheduler   periodic_scheduler.PeriodicScheduler
}

func NewSyncStrategyPrtg(
	agentConfig configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	baseLogger *logger.Logger,
) *SyncStrategyPrtg {
	// Create module-specific logger for PRTG strategy
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.prtg")
	http := httpretry.NewDefaultClient(
		// retry up to 3 times
		httpretry.WithMaxRetryCount(3),
	)
	http.Timeout = defaultPushTimeout

	strategy := SyncStrategyPrtg{
		buffer:      NewBuffer(),
		http:        http,
		rawConfig:   storageConfig,
		agentConfig: agentConfig,
		logger:      moduleLogger,
	}

	return &strategy
}

func ParseSyncStrategyPrtgParams(config configuration.StorageConfigParams) (SyncStrategyPrtgParams, error) {
	errs := []error{}
	params := SyncStrategyPrtgParams{
		Interval:        DEFAULT_PRTG_INTERVAL,
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

	url, ok := config["server_url"].(string)
	if !ok || url == "" {
		errs = append(errs, fmt.Errorf("server_url parameter is required"))
	} else if !validators.IsURL(url) {
		errs = append(errs, fmt.Errorf("server_url must be a valid URL"))
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

func (s *SyncStrategyPrtg) GetStrategyParams() map[string]interface{} {
	return s.rawConfig
}

func (s *SyncStrategyPrtg) AddDataPoints(data []datapoint.DataPoint) error {
	if err := s.buffer.Append(data); err != nil {
		return fmt.Errorf("failed to append data to buffer: %w", err)
	}
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
	if (s.scheduler) != nil {
		return nil
	}
	scheduler := periodic_scheduler.NewPeriodicScheduler(periodic_scheduler.PeriodicSchedulerConfig{
		Interval:          s.config.Interval,
		Execute:           s.DoSync,
		ExecuteOnStart:    false,
		ExecuteOnShutdown: true,
	}, s.logger.Logger)
	s.scheduler = scheduler
	return s.scheduler.Start(nil)
}

func (s *SyncStrategyPrtg) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down sync strategy")
	defer func() {
		s.scheduler = nil
	}()
	return s.scheduler.Shutdown(ctx)
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

func metricId(p datapoint.DataPoint) string {
	// Find a tag named "metric_id"
	for _, t := range p.Tags {
		if t.Key == "prtg_metric_id" {
			return strings.Replace(t.Value, "[name]", p.Name, -1)
		}
	}

	// Fallback to the name
	return p.Name
}

func (s *SyncStrategyPrtg) DoSync() error {
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

	data = filter(data, func(p datapoint.DataPoint) bool {
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

	// Some probes might not read any value until the next sync, so valid data
	// points are restored in the buffer.
	// This happens wether the sync is successful or not.
	if abortErr := s.buffer.AbortSync(data); abortErr != nil {
		s.logger.Error().Err(abortErr).Msg("failed to abort sync")
	}
	if err := s.doSyncData(data); err != nil {
		s.logger.Error().Err(err).Msg("error synchronizing data")
		return err
	}

	return nil
}

type PrtgResult struct {
	Channel string  `json:"channel"`
	Value   float64 `json:"value"`
	Float   int     `json:"float"`
}

type PrtgData struct {
	Prtg struct {
		Result []PrtgResult `json:"result"`
	} `json:"prtg"`
}

func (s *SyncStrategyPrtg) doSyncData(data []datapoint.DataPoint) error {
	// Transform data points to PRTG format
	jsonData := PrtgData{}
	for _, p := range data {
		jsonData.Prtg.Result = append(jsonData.Prtg.Result, PrtgResult{
			metricId(p),
			p.Value,
			1,
		})
	}

	// Send data to PRTG
	requestBody, err := json.Marshal(jsonData)
	if err != nil {
		s.logger.Error().Err(err).Msg("error encoding data.")
		return err
	}
	resp, err := s.http.Post(
		s.config.ServerUrl,
		"application/json",
		bytes.NewBuffer(requestBody),
	)
	if err != nil {
		return err
	}
	// Drain + close so the transport can reuse the connection; this
	// push runs every sync and leaked one connection per cycle (#277).
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status code: %d: %s", resp.StatusCode, body)
	}

	return nil
}
