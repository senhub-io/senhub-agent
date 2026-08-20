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
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/pushqueue"
	"senhub-agent.go/internal/agent/services/exporterrors"
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
	// failedEvents is the retry backlog for batches the intake refused.
	// It is the shared bounded queue rather than a plain slice: an
	// unbounded retry backlog is the OOM class the two metric sinks
	// closed in #267, and this sink was the one place it was still live
	// — every failed sync appended the whole batch with no cap, so an
	// intake outage grew it until the process died (#287).
	failedEvents *pushqueue.Bounded[eventtypes.EventDataPoint]

	syncTriggerSize  int   // Number of events that triggers a sync
	syncTriggerBytes int64 // Size in bytes that triggers a sync

	// retryAttempts / retryDelay drive the short in-tick retry. They are
	// fields rather than the bare constants so a test can exercise the
	// outage path without paying seconds of real sleep per cycle.
	retryAttempts uint
	retryDelay    time.Duration

	config      EventSyncStrategyParams
	server      server.Server
	logger      *logger.ModuleLogger
	ticker      *time.Ticker
	tickerOnce  sync.Once
	tickerStop  chan struct{}
	stopOnce    sync.Once
	agentConfig configuration.AgentConfiguration
	formatter   *eventFormatter.Formatter

	// Log-bus pump (#294 step 1a): syslog events now ride the agent log
	// bus, not the metric datapoint path. The pump drains the log channel,
	// keeps only syslog records, converts each to the same EventDataPoint
	// FormatDataPoint produced, and enqueues it — so /event/insert is
	// byte-identical. The event (HTTP) probe still feeds AddDataPoints
	// until its structured payloads are carried on the log bus (step 1b).
	logSub      <-chan agentstate.LogRecord
	logCancel   context.CancelFunc
	logWG       sync.WaitGroup
	logPumpOnce sync.Once
}

// NewEventSyncStrategy creates a new instance of EventSyncStrategy.
// A missing or non-string server_url is a configuration error, not a
// panic: the unchecked type assertion here used to crash the agent at
// config load, before ValidateConfigParams ever ran (#261).
func NewEventSyncStrategy(
	agentConfig configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	baseLogger *logger.Logger,
) (*EventSyncStrategy, error) {
	// Create module-specific logger for event strategy
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.event")

	serverURL, ok := storageConfig["server_url"].(string)
	if !ok || serverURL == "" {
		return nil, fmt.Errorf("event strategy requires a string server_url parameter (got %T)", storageConfig["server_url"])
	}

	srv := server.NewServer(
		agentConfig.GetAuthenticationKey(),
		serverURL,
		baseLogger,
	)

	// Default configuration
	config := EventSyncStrategyParams{
		QueueSize:    DefaultQueueSize,
		SyncInterval: DefaultSyncInterval,
	}

	config.ServerURL = serverURL
	config.ServerURLFull = serverURL + "/event/insert"
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
		failedEvents:     pushqueue.NewDefault[eventtypes.EventDataPoint]("event"),
		config:           config,
		server:           srv,
		agentConfig:      agentConfig,
		logger:           moduleLogger,
		formatter:        eventFormatter.NewFormatter(),
		retryAttempts:    DefaultRetryAttempts,
		retryDelay:       DefaultRetryDelay,
		syncTriggerSize:  DefaultChunkSize,
		syncTriggerBytes: MaxMessageSize / 2, // Trigger at 50% of max message size
	}

	// Initialize atomic values
	strategy.currentSize.Store(0)
	strategy.syncInProgress.Store(false)

	return strategy, nil
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
		s.enqueue(s.formatter.FormatDataPoint(dp))
	}
	return nil
}

// enqueue validates a formatted event and pushes it onto the buffer,
// triggering a sync at the size/byte thresholds. On a full buffer it drops
// the oldest event to make room (best-effort, same posture as the receive
// side). Shared by AddDataPoints (event probe datapoints) and the log-bus
// pump (syslog records) so both sources behave identically.
func (s *EventSyncStrategy) enqueue(evt eventtypes.EventDataPoint) {
	if err := evt.Validate(); err != nil {
		s.logger.Error().Err(err).Msg("Invalid event data")
		return
	}

	eventJson, err := json.Marshal(evt)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to marshal event")
		return
	}
	eventSize := int64(len(eventJson))

	select {
	case s.buffer <- evt:
		newSize := s.currentSize.Add(eventSize)
		if len(s.buffer) >= s.syncTriggerSize || newSize >= s.syncTriggerBytes {
			s.triggerSync()
		}
	default:
		s.logger.Warn().Msg("Buffer full, attempting to make room")
		select {
		case <-s.buffer: // Remove oldest event
			select {
			case s.buffer <- evt:
				s.logger.Warn().Msg("Dropped oldest event to make room")
			default:
				// The freed slot was taken by a concurrent producer. Drop
				// rather than block: a blocking send here can park the pump
				// goroutine so Shutdown's logWG.Wait() stalls until the next
				// sync drains the buffer (audit m9). enqueue must never block.
				s.logger.Warn().Msg("Buffer still full after eviction; event lost")
			}
		default:
			s.logger.Error().Msg("Failed to make room in buffer, event lost")
		}
	}
}

// drainLogSub processes any records still buffered in the subscription
// channel after the pump has stopped. Non-blocking: returns once the channel
// is empty. Called from Shutdown after UnsubscribeLogs (no new sends arrive).
func (s *EventSyncStrategy) drainLogSub() {
	for {
		select {
		case rec := <-s.logSub:
			s.convertAndEnqueue(rec)
		default:
			return
		}
	}
}

// convertAndEnqueue converts one syslog/event log record to the /event/insert
// format and enqueues it. Shared by the pump and the shutdown drain.
func (s *EventSyncStrategy) convertAndEnqueue(rec agentstate.LogRecord) {
	switch rec.ProducerProbeType {
	case "syslog":
		s.enqueue(s.withGlobalTags(s.formatter.FromSyslogLog(rec)))
	case "event":
		s.enqueue(s.withGlobalTags(s.formatter.FromEventLog(rec)))
	}
}

// withGlobalTags overlays the agent-level global_tags onto an event so
// /event/insert stays byte-identical to the legacy metric datapoint path,
// which flowed through the DataStore's enrichWithConfiguredTags (global_tags
// merged into every datapoint, then emitted as fields by FormatDataPoint).
// The log bus bypasses the DataStore, so re-apply them here. global_tags win
// on a key conflict, matching the old MergeTags precedence (audit M2).
// Per-probe custom_tags were already inert for syslog/event (their datapoints
// carried no probe_name tag), so they are not re-applied.
func (s *EventSyncStrategy) withGlobalTags(evt eventtypes.EventDataPoint) eventtypes.EventDataPoint {
	for k, v := range s.agentConfig.GetGlobalTags() {
		evt[k] = v
	}
	return evt
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
	if retryBacklog := s.failedEvents.Sync(); len(retryBacklog) > 0 {
		s.logger.Info().
			Int("count", len(retryBacklog)).
			Msg("Processing previously failed events")
		events = append(events, retryBacklog...)
	}

	// Collect events up to chunk limits. The breaks must exit the
	// LOOP, not just the select: an unlabeled break here caused an
	// infinite busy-spin once a batch exceeded the size limit — the
	// same event was re-received and put back forever, with
	// syncInProgress stuck true and the event pipeline dead (#261).
collect:
	for len(events) < s.syncTriggerSize && len(s.buffer) > 0 {
		select {
		case evt := <-s.buffer:
			eventJson, err := json.Marshal(evt)
			if err != nil {
				s.logger.Error().Err(err).Msg("Failed to marshal event during sync")
				continue
			}

			if currentBatchSize+int64(len(eventJson)) > s.syncTriggerBytes {
				// Put the event back if it would exceed size limit
				s.buffer <- evt
				break collect
			}

			currentBatchSize += int64(len(eventJson))
			events = append(events, evt)
		default:
			break collect
		}
	}

	// currentSize tracks bytes RESIDENT in the buffer: subtract what
	// this collection drained, regardless of the send outcome —
	// failed events move to failedEvents (outside the buffer) and
	// must not keep inflating the size-based sync trigger.
	s.currentSize.Add(-currentBatchSize)

	if len(events) == 0 {
		return nil
	}

	// Try to send events with retry mechanism. RetryIf keeps the short
	// in-tick retry for a transport blip but skips it entirely for a
	// payload the intake refused on its merits: three attempts and two
	// seconds of sleep inside the scheduler tick buy nothing when the
	// same bytes get the same rejection.
	err := retry.Do(
		func() error {
			return s.sendEvents(events)
		},
		retry.Attempts(s.retryAttempts),
		retry.Delay(s.retryDelay),
		retry.RetryIf(exporterrors.IsRetryable),
		retry.OnRetry(func(n uint, err error) {
			s.logger.Warn().
				Err(err).
				Uint("attempt", n+1).
				Int("events_count", len(events)).
				Msg("Retrying event sync")
		}),
	)

	if err != nil {
		agentstate.IncrementExportSendFailed("event", exporterrors.Reason(err))

		if !exporterrors.IsRetryable(err) {
			// Nothing a later tick can change. Keeping the batch would
			// pin it at the head of the retry backlog forever, so the
			// backlog never drains and every tick re-sends bytes the
			// intake already refused.
			s.logger.Warn().
				Err(err).
				Int("dropped_events", len(events)).
				Msg("unrecoverable error from intake; discarding events (no retry)")
			agentstate.IncrementPushBufferDropped("event", len(events))
			return nil
		}

		// Preserve failed events for next sync attempt. The backlog is
		// bounded: past the cap the oldest events go, which is what an
		// outage longer than the queue depth costs.
		if abortErr := s.failedEvents.AbortSync(events); abortErr != nil {
			s.logger.Error().Err(abortErr).Msg("failed to queue events for retry")
		}
		return fmt.Errorf("failed to sync events after %d attempts: %w", s.retryAttempts, err)
	}

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
		// The agent could not even serialise what it holds: retrying the
		// same values produces the same failure.
		return exporterrors.Validation("marshaling events array", err)
	}

	s.logger.Debug().
		Int("event_count", len(events)).
		Int("payload_size", len(eventsJSON)).
		Msg("Sending batch of events")

	response, err := s.server.PostStream("/event/insert", string(eventsJSON))
	if err != nil {
		// The far end was not reached. Keep the batch.
		return exporterrors.Transport("sending events", err)
	}
	defer response.Body.Close()

	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return exporterrors.Transport("reading response body", err)
	}

	// Accept both 200 OK and 202 Accepted as successful responses
	if response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusOK {
		statusErr := fmt.Errorf("unexpected status code: %d - body: %s", response.StatusCode, string(respBody))
		// Same split as the cloud metrics sink: a 4xx is permanent
		// except the ones a resend plausibly recovers from (408, 429,
		// and the auth pair, which clears on an intake blip or a slow
		// key rotation). 5xx and everything else stays retryable.
		if exporterrors.IsPermanentHTTPStatus(response.StatusCode) {
			return exporterrors.Validation("intake rejected the batch", statusErr)
		}
		return exporterrors.Transport("intake did not accept the batch", statusErr)
	}

	s.logger.Info().
		Int("status_code", response.StatusCode).
		Int("event_count", len(events)).
		Msg("Server confirmed receipt of events")

	return nil
}

// Start initializes and starts the sync strategy
func (s *EventSyncStrategy) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.tickerOnce.Do(func() {
		s.ticker = time.NewTicker(s.config.SyncInterval)
		s.tickerStop = make(chan struct{})
		// Agent-wide cancellation stops the ticker goroutine even when
		// nothing calls Shutdown; stopOnce keeps the two paths from
		// double-closing.
		context.AfterFunc(ctx, func() {
			s.stopOnce.Do(func() { close(s.tickerStop) })
		})
		s.logger.Info().
			Dur("interval", s.config.SyncInterval).
			Int("queue_size", s.config.QueueSize).
			Msg("Starting event sync strategy")

		// ticker.Stop() does not close ticker.C, so a bare
		// `for range ticker.C` never exits after Shutdown — the
		// goroutine (and the strategy it captures) leaks (#270).
		go func(ticker *time.Ticker, stop chan struct{}) {
			for {
				select {
				case <-ticker.C:
					s.triggerSync()
				case <-stop:
					return
				}
			}
		}(s.ticker, s.tickerStop)
	})
	s.startLogPump(ctx)
	return nil
}

// startLogPump subscribes to the agent log bus and forwards the two
// /event/insert producers — syslog and event — to the legacy rail
// (#294 step 1a/1b). Idempotent. Every OTHER log producer (filetail,
// linux_logs, snmp_trap, otlp_receiver, …) is skipped so it does NOT leak
// onto /event/insert. Each record is converted with the format-preserving
// FromSyslogLog / FromEventLog so the payload is byte-identical to the old
// metric datapoint path.
func (s *EventSyncStrategy) startLogPump(parent context.Context) {
	s.logPumpOnce.Do(func() {
		ch := agentstate.SubscribeLogs(s.config.QueueSize)
		s.logSub = ch
		ctx, cancel := context.WithCancel(parent)
		s.logCancel = cancel
		s.logWG.Add(1)
		go func() {
			defer s.logWG.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case rec, ok := <-ch:
					if !ok {
						return
					}
					switch rec.ProducerProbeType {
					default:
						// Not an /event/insert producer — skip so other log
						// sources (filetail, linux_logs, snmp_trap, …) never
						// leak onto the legacy rail.
						continue
					case "syslog", "event":
						s.convertAndEnqueue(rec)
					}
				}
			}
		}()
	})
}

// Shutdown performs a graceful shutdown of the sync strategy
func (s *EventSyncStrategy) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Initiating graceful shutdown")

	// Stop the log-bus pump first so no new syslog events arrive while we
	// drain. Unsubscribe follows the same #262 contract as the OTLP pump:
	// the channel is never closed here, the pump exits via the cancel.
	if s.logCancel != nil {
		s.logCancel()
		s.logWG.Wait()
		agentstate.UnsubscribeLogs(s.logSub)
		// Drain records already accepted into the subscription buffer but not
		// yet processed by the now-stopped pump, so a burst arriving just
		// before stop is not silently lost — the old synchronous path
		// included such events in the final flush (audit m10). Unsubscribe
		// above stopped new deliveries, so this non-blocking drain terminates.
		s.drainLogSub()
	}

	if s.ticker != nil {
		s.ticker.Stop()
	}
	if s.tickerStop != nil {
		s.stopOnce.Do(func() { close(s.tickerStop) })
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
