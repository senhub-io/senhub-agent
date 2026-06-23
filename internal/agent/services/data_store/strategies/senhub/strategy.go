// senhub-agent/internal/agent/services/data_store/stategy_senhub.go
package senhub

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/validators"
)

var (
	DEFAULT_SENHUB_INTERVAL = 5 * time.Second
)

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

// DefaultMaxBufferPoints bounds the cloud push buffer. Before the cap
// an intake outage grew the buffer until OOM (#267, audit A3): every
// failed sync re-prepended the whole backlog while collection kept
// appending. 100k points is hours of typical agent volume; oldest
// points are dropped first — the freshest data is the valuable part
// of a monitoring stream when the backlog cannot be shipped anyway.
const DefaultMaxBufferPoints = 100000

// NewBuffer creates a buffer bounded at DefaultMaxBufferPoints.
func NewBuffer() Buffer {
	return NewBufferWithCap(DefaultMaxBufferPoints)
}

// NewBufferWithCap creates a buffer bounded at maxPoints (0 = unbounded).
func NewBufferWithCap(maxPoints int) Buffer {
	return &buffer{
		data:      &[]datapoint.DataPoint{},
		maxPoints: maxPoints,
	}
}

// trimToCap drops the OLDEST points so the buffer holds at most
// maxPoints, recording the drops. Callers hold the mutex.
func (b *buffer) trimToCap() {
	if b.maxPoints <= 0 || len(*b.data) <= b.maxPoints {
		return
	}
	dropped := len(*b.data) - b.maxPoints
	*b.data = (*b.data)[dropped:]
	agentstate.IncrementPushBufferDropped("senhub", dropped)
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

// AbortSync re-prepends failed data (oldest first) so ordering
// survives a retry; the cap then trims from the oldest end.
func (b *buffer) AbortSync(failedData []datapoint.DataPoint) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	*b.data = append(failedData, *b.data...)
	b.trimToCap()
	return nil
}

type SenhubDataPoint struct {
	Name      string     `json:"name"`
	Timestamp time.Time  `json:"timestamp"`
	Value     float64    `json:"value"`
	Tags      []tags.Tag `json:"tags,omitempty"`
}

type SyncStrategySenhubParams struct {
	Interval  time.Duration
	ServerUrl string
}

// Synchronize metrics to senhub backend.
type SyncStrategySenhub struct {
	buffer        Buffer
	agentConfig   configuration.AgentConfiguration
	storageConfig configuration.StorageConfigParams
	server        server.Server // Utilise la nouvelle interface
	logger        *logger.ModuleLogger
	config        SyncStrategySenhubParams
	scheduler     periodic_scheduler.PeriodicScheduler
}

func NewSyncStrategySenhub(
	agentConfig configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	baseLogger *logger.Logger,
) interface{} {
	// Create module-specific logger for SenHub strategy
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.senhub")

	// The cloud intake URL is injected at build time (see Makefile
	// ldflags). Operators who need a non-default intake (staging / dev)
	// get the alternate URL from the build env.
	srv := server.NewServer(
		agentConfig.GetAuthenticationKey(),
		cliArgs.ProductionURL,
		baseLogger,
	)

	return &SyncStrategySenhub{
		buffer:        NewBuffer(),
		agentConfig:   agentConfig,
		storageConfig: storageConfig,
		logger:        moduleLogger,
		server:        srv,
	}
}

func (s *SyncStrategySenhub) GetStrategyName() string {
	return "senhub"
}

func (s *SyncStrategySenhub) GetStrategyParams() map[string]interface{} {
	return s.storageConfig
}

func (s *SyncStrategySenhub) AddDataPoints(data []datapoint.DataPoint) error {
	if err := s.buffer.Append(data); err != nil {
		return fmt.Errorf("failed to append data to buffer: %w", err)
	}
	return nil
}

func ParseSyncStrategySenhubParams(config configuration.StorageConfigParams) (SyncStrategySenhubParams, error) {
	errs := []error{}
	params := SyncStrategySenhubParams{
		Interval: DEFAULT_SENHUB_INTERVAL,
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

	if len(errs) > 0 {
		return params, fmt.Errorf("error parsing config: %v", errs)
	}

	return params, nil
}
func (s *SyncStrategySenhub) ValidateConfigParams(params configuration.StorageConfigParams) error {
	config, err := ParseSyncStrategySenhubParams(params)
	if err != nil {
		return err
	}

	s.config = config
	return nil
}

func (s *SyncStrategySenhub) Start() error {
	if (s.scheduler) != nil {
		return nil
	}
	scheduler := periodic_scheduler.NewPeriodicScheduler(periodic_scheduler.PeriodicSchedulerConfig{
		Interval:          s.config.Interval,
		Execute:           s.doSync,
		ExecuteOnStart:    false,
		ExecuteOnShutdown: true,
	}, s.logger.Logger)
	s.scheduler = scheduler

	return s.scheduler.Start(nil)
}

func (s *SyncStrategySenhub) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down sync strategy")
	defer func() {
		s.scheduler = nil
	}()
	return s.scheduler.Shutdown(ctx)
}

func (s *SyncStrategySenhub) doSync() error {
	data := s.buffer.Sync()
	if len(data) == 0 {
		return nil
	}

	// Remove private tags
	transformedData := make([]SenhubDataPoint, 0, len(data))
	for _, dp := range data {

		transformedData = append(transformedData, SenhubDataPoint{
			Name:      dp.Name,
			Timestamp: dp.Timestamp,
			Value:     dp.Value,
			Tags: tags.FormatTagsForServer(
				tags.OnlyPublicTags(dp.Tags),
			),
		})
	}

	s.logger.Debug().Any("data", transformedData).Msg("synchronizing data")
	if err := s.doSyncData(transformedData); err != nil {
		s.logger.Error().Err(err).Msg("error synchronizing data")
		if abortErr := s.buffer.AbortSync(data); abortErr != nil {
			s.logger.Error().Err(abortErr).Msg("failed to abort sync")
		}
		return err
	}

	return nil
}

func (s *SyncStrategySenhub) doSyncData(data []SenhubDataPoint) error {
	response, err := s.server.Post("/metrics", data)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d\n%v", response.StatusCode, response.Body)
	}

	return nil
}
