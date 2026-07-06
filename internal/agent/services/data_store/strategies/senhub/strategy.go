// senhub-agent/internal/agent/services/data_store/stategy_senhub.go
package senhub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
		var permErr *permanentClientError
		if errors.As(err, &permErr) {
			// A permanent 4xx (e.g. 400 malformed, 422 unprocessable)
			// will never be accepted no matter how often we resend it.
			// Re-prepending it via AbortSync would pin the batch at the
			// head of the buffer forever, so the buffer never drains and
			// every scheduler tick wastes a round-trip. Drop it instead.
			s.logger.Warn().
				Int("status_code", permErr.statusCode).
				Int("dropped_points", len(data)).
				Msg("permanent client error from intake; discarding batch (no retry)")
			agentstate.IncrementPushBufferDropped("senhub", len(data))
			return nil
		}
		s.logger.Error().Err(err).Msg("error synchronizing data")
		if abortErr := s.buffer.AbortSync(data); abortErr != nil {
			s.logger.Error().Err(abortErr).Msg("failed to abort sync")
		}
		return err
	}

	return nil
}

// permanentClientError marks an intake response that must not be retried:
// the payload is rejected for a reason resending cannot fix.
type permanentClientError struct {
	statusCode int
}

func (e *permanentClientError) Error() string {
	return fmt.Sprintf("permanent client error: status %d", e.statusCode)
}

// isPermanentClientStatus reports whether a 4xx status is a permanent
// client error for a metrics push. Every 4xx is treated as permanent
// except the ones a resend can plausibly recover from:
//   - 408 (Request Timeout) and 429 (Too Many Requests): transient by
//     definition.
//   - 401 (Unauthorized) and 403 (Forbidden): an intake-side auth blip or a
//     slow key-rotation propagation clears on its own; dropping the batch would
//     lose data during a window a retry would ride out. The bounded push buffer
//     caps the backlog if the key is genuinely bad, so retrying is safe.
//
// Non-4xx (network errors, 5xx) are never classified here and keep their
// existing retry behavior.
func isPermanentClientStatus(status int) bool {
	if status < 400 || status >= 500 {
		return false
	}
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusUnauthorized, http.StatusForbidden:
		return false
	default:
		return true
	}
}

func (s *SyncStrategySenhub) doSyncData(data []SenhubDataPoint) error {
	response, err := s.server.Post("/metrics", data)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		if isPermanentClientStatus(response.StatusCode) {
			return &permanentClientError{statusCode: response.StatusCode}
		}
		return fmt.Errorf("unexpected status code: %d\n%v", response.StatusCode, response.Body)
	}

	return nil
}
