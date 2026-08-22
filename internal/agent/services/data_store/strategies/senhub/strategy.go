// senhub-agent/internal/agent/services/data_store/stategy_senhub.go
package senhub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/pushqueue"
	"senhub-agent.go/internal/agent/services/exporterrors"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/validators"
)

var (
	DEFAULT_SENHUB_INTERVAL = 5 * time.Second
)

// Buffer is the backlog contract this strategy uses. The implementation
// is the shared bounded queue: this used to be a verbatim fork of the
// same code the PRTG sink carried, and a fork is how the two drifted
// (#287).
type Buffer interface {
	Append(newData []datapoint.DataPoint) error
	Sync() []datapoint.DataPoint
	AbortSync(failedData []datapoint.DataPoint) error
	Len() int
}

// DefaultMaxBufferPoints bounds the cloud push buffer. Before the cap
// an intake outage grew the buffer until OOM (#267, audit A3): every
// failed sync re-prepended the whole backlog while collection kept
// appending.
const DefaultMaxBufferPoints = pushqueue.DefaultMaxItems

// NewBuffer creates a buffer bounded at DefaultMaxBufferPoints.
func NewBuffer() Buffer {
	return NewBufferWithCap(DefaultMaxBufferPoints)
}

// NewBufferWithCap creates a buffer bounded at maxPoints (0 = unbounded).
func NewBufferWithCap(maxPoints int) Buffer {
	return pushqueue.New[datapoint.DataPoint]("senhub", maxPoints)
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
		return params, fmt.Errorf("error parsing config: %w", errors.Join(errs...))
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

func (s *SyncStrategySenhub) Start(ctx context.Context) error {
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

	return s.scheduler.Start(ctx)
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
		agentstate.IncrementExportSendFailed("senhub", exporterrors.Reason(err))

		if !exporterrors.IsRetryable(err) {
			// Nothing a later tick can change: a permanent 4xx (400
			// malformed, 422 unprocessable), a payload we cannot even
			// serialise, or an endpoint URL the operator has to fix.
			// Re-prepending the batch via AbortSync would pin it at the
			// head of the buffer forever, so the buffer never drains and
			// every scheduler tick wastes a round-trip. Drop it instead.
			event := s.logger.Warn()
			var permErr *permanentClientError
			if errors.As(err, &permErr) {
				event = event.Int("status_code", permErr.statusCode)
			}
			event.
				Err(err).
				Int("dropped_points", len(data)).
				Msg("unrecoverable error from intake; discarding batch (no retry)")
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
	// reason is what the intake said. Empty when it said nothing.
	reason string
}

func (e *permanentClientError) Error() string {
	if e.reason == "" {
		return fmt.Sprintf("permanent client error: status %d", e.statusCode)
	}
	return fmt.Sprintf("permanent client error: status %d: %s", e.statusCode, e.reason)
}

// Unwrap places the status-code detail inside the shared taxonomy: the
// intake looked at what we sent and refused it, which is exactly
// ErrValidation. Callers that only need "retry or drop" ask
// exporterrors.IsRetryable; the ones that want the status code still
// reach it with errors.As.
func (e *permanentClientError) Unwrap() error { return exporterrors.ErrValidation }

// maxRejectionBodyBytes bounds how much of a refusal the agent reads
// back. A rejection carries a sentence, not a stream; reading without a
// bound would let a misbehaving endpoint stream into a log line.
const maxRejectionBodyBytes = 4096

func (s *SyncStrategySenhub) doSyncData(data []SenhubDataPoint) error {
	response, err := s.server.Post("/metrics", data)
	if err != nil {
		return err
	}
	// Drain and close so the transport can reuse the connection. The
	// body was never closed here, on any path — one leaked connection
	// per sync, the same defect PRTG had in #277.
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	if response.StatusCode != 200 {
		// The intake's own explanation of the refusal, which is the only
		// thing that matters on this failure. It used to be formatted
		// with %v against the io.ReadCloser, so the log carried the
		// pointer — "&{0xc000...}" — and never the reason (#832).
		reason := readRejectionReason(response.Body)
		if exporterrors.IsPermanentHTTPStatus(response.StatusCode) {
			return &permanentClientError{statusCode: response.StatusCode, reason: reason}
		}
		if reason == "" {
			return fmt.Errorf("unexpected status code: %d", response.StatusCode)
		}
		return fmt.Errorf("unexpected status code: %d: %s", response.StatusCode, reason)
	}

	return nil
}

// readRejectionReason reads a bounded, single-line rendering of an error
// body. Returns "" when the body is empty or unreadable — the status
// code alone is still worth reporting, so a read failure must not lose
// the error it was describing.
func readRejectionReason(body io.Reader) string {
	if body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxRejectionBodyBytes))
	if err != nil {
		return ""
	}
	// Collapse to one line: this lands in a structured log field, and a
	// multi-line body would break the record it is embedded in.
	return strings.TrimSpace(strings.Join(strings.Fields(string(raw)), " "))
}
