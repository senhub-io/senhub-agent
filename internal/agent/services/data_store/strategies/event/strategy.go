package event

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/avast/retry-go/v4"

	eventFormatter "senhub-agent.go/internal/agent/formats/event"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/pushqueue"
	"senhub-agent.go/internal/agent/services/exporterrors"
	"senhub-agent.go/internal/agent/services/instanceid"
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
	// DefaultFirstRetryDelay holds the first resend of a batch whose answer
	// was lost until the intake can see the ids it already stored (about
	// one second after insertion); an earlier resend is stored again.
	DefaultFirstRetryDelay = 5 * time.Second
	// DefaultFirstRetryJitter spreads first resends from many agents.
	DefaultFirstRetryJitter = 2 * time.Second
	// backlogWarnEvery spaces the warnings about a backlog that cannot
	// be written.
	backlogWarnEvery = time.Minute
)

// EventSyncStrategyParams holds the configuration for the event sync strategy
type EventSyncStrategyParams struct {
	ServerURL     string        // Base URL for the server
	ServerURLFull string        // Full URL including the endpoint path
	QueueSize     int           // Size of the event buffer
	SyncInterval  time.Duration // Interval between syncs

	// BacklogPath is the directory holding undelivered batches; empty
	// means event-queue under the agent state directory.
	BacklogPath string
	// BacklogMaxBytes caps the disk used by undelivered batches.
	BacklogMaxBytes int64
	// BacklogMaxAge drops undelivered batches older than this.
	BacklogMaxAge time.Duration
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
	// backlog is the durable retry queue. nil until Start has proved the
	// directory writable, and for good when it is not: the strategy then
	// runs on failedEvents alone.
	backlog     *eventBacklog
	backlogOnce sync.Once
	lastWarnAt  atomic.Int64 // unix nanoseconds of the last backlog write warning
	unwarned    atomic.Int64 // events lost to write failures since that warning

	// persistMu serialises every write of the backlog from the sync path
	// and from Shutdown, so the order on disk is the order of arrival:
	// the batch a sync holds, then the buffer. inflight is that batch
	// while its send is under way; closing means Shutdown has taken over
	// and no sync may write any more.
	persistMu sync.Mutex
	inflight  []eventtypes.EventDataPoint
	closing   bool

	// syncCtx bounds the background syncs. Shutdown cancels it when its
	// own deadline passes, so a sync stuck on an intake that does not
	// answer gives its batch to the backlog instead of being abandoned.
	syncCtx    context.Context
	syncCancel context.CancelFunc

	syncTriggerSize  int   // Number of events that triggers a sync
	syncTriggerBytes int64 // Size in bytes that triggers a sync

	// retryAttempts / retryDelay drive the short in-tick retry. They are
	// fields rather than the bare constants so a test can exercise the
	// outage path without paying seconds of real sleep per cycle.
	retryAttempts    uint
	retryDelay       time.Duration
	firstRetryDelay  time.Duration
	firstRetryJitter time.Duration

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
	if err := parseBacklogParams(storageConfig, &config); err != nil {
		return nil, err
	}

	syncCtx, syncCancel := context.WithCancel(context.Background())
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
		firstRetryDelay:  DefaultFirstRetryDelay,
		firstRetryJitter: DefaultFirstRetryJitter,
		syncTriggerSize:  DefaultChunkSize,
		syncTriggerBytes: MaxMessageSize / 2, // Trigger at 50% of max message size
		syncCtx:          syncCtx,
		syncCancel:       syncCancel,
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

// ValidateParams reads the parameters without touching any strategy.
//
// Kept separate from the method below because a caller that only wants
// to know whether a configuration is acceptable — `agent config check`
// — must not resize a live buffer to find out, and could not call the
// method at all: on a zero-value strategy it closes a nil channel
// (#848).
func ValidateParams(params configuration.StorageConfigParams) (EventSyncStrategyParams, error) {
	config := EventSyncStrategyParams{
		QueueSize:    DefaultQueueSize,
		SyncInterval: DefaultSyncInterval,
	}

	if url, ok := params["server_url"].(string); !ok || url == "" {
		return config, fmt.Errorf("server_url is required")
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
			return config, fmt.Errorf("invalid sync_interval: %w", err)
		}
		config.SyncInterval = duration
	}

	if err := parseBacklogParams(params, &config); err != nil {
		return config, err
	}

	return config, nil
}

// parseBacklogParams reads the three keys of the durable backlog.
func parseBacklogParams(params configuration.StorageConfigParams, config *EventSyncStrategyParams) error {
	if v, ok := params["backlog_path"].(string); ok {
		config.BacklogPath = v
	}
	if raw, ok := params["backlog_max_bytes"]; ok {
		var n int64
		switch v := raw.(type) {
		case int:
			n = int64(v)
		case int64:
			n = v
		case uint64:
			n = int64(v)
		case float64:
			n = int64(v)
		default:
			return fmt.Errorf("backlog_max_bytes must be a number of bytes, got %T", raw)
		}
		if n < 0 {
			return fmt.Errorf("backlog_max_bytes must be positive, got %d", n)
		}
		config.BacklogMaxBytes = n
	}
	if raw, ok := params["backlog_max_age"]; ok {
		str, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("backlog_max_age must be a duration such as 24h, got %T", raw)
		}
		d, err := time.ParseDuration(str)
		if err != nil {
			return fmt.Errorf("invalid backlog_max_age: %w", err)
		}
		if d < 0 {
			return fmt.Errorf("backlog_max_age must be positive, got %s", str)
		}
		config.BacklogMaxAge = d
	}
	return nil
}

// openBacklog starts the durable retry queue. A directory that cannot be
// written leaves the strategy on its in-memory backlog, with one warning:
// an agent whose state directory is read-only still has to push events.
func (s *EventSyncStrategy) openBacklog() {
	s.backlogOnce.Do(func() {
		dir := s.config.BacklogPath
		if dir == "" {
			dir = filepath.Join(instanceid.OwnStateDir(), backlogDirName)
		}
		if err := prepareBacklogDir(dir); err != nil {
			s.logger.Warn().Err(err).Str("path", dir).
				Msg("Event backlog disabled: the directory is not writable; undelivered events are kept in memory only and lost on restart")
			return
		}
		b := newEventBacklog(dir, s.config.BacklogMaxBytes, s.config.BacklogMaxAge, s.logger)
		b.sweep()
		pending, size := b.pending()
		s.logger.Info().Str("path", dir).Int64("max_bytes", b.maxBytes).Dur("max_age", b.maxAge).
			Int("pending_events", pending).Int64("pending_bytes", size).
			Msg("Event backlog active: undelivered events are kept on disk and replayed when the intake answers")
		s.backlog = b
	})
}

// ValidateConfigParams validates the provided configuration parameters
// and adopts them.
func (s *EventSyncStrategy) ValidateConfigParams(params configuration.StorageConfigParams) error {
	config, err := ValidateParams(params)
	if err != nil {
		return err
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
// eventIDField carries a random id set once when an event is queued. It
// rides every resend of the event, so an intake that inserted a batch and
// lost its answer can recognise the copies.
const eventIDField = "senhub_event_id"

func generateEventID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *EventSyncStrategy) enqueue(evt eventtypes.EventDataPoint) {
	if id, ok := evt[eventIDField].(string); !ok || id == "" {
		if generated, err := generateEventID(); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to generate event ID; sending without one")
		} else {
			evt[eventIDField] = generated
		}
	}

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
			for {
				before := s.backlogFiles()
				if err := s.doSync(s.syncCtx); err != nil {
					s.logger.Error().
						Err(err).
						Msg("Sync failed, events preserved for retry")
					return
				}
				// A backlog that is draining is served again at once rather
				// than one slice per tick; no progress ends the loop.
				if after := s.backlogFiles(); after == 0 || after >= before {
					return
				}
			}
		}()
	}
}

// backlogFiles is the number of batches waiting on disk.
func (s *EventSyncStrategy) backlogFiles() int {
	if s.backlog == nil {
		return 0
	}
	return s.backlog.files()
}

// collectBatch takes the next batch off the buffer, within the chunk
// limits. The breaks must exit the LOOP, not just the select: an unlabeled
// break here caused an infinite busy-spin once a batch exceeded the size
// limit — the same event was re-received and put back forever, with
// syncInProgress stuck true and the event pipeline dead (#261).
func (s *EventSyncStrategy) collectBatch(events []eventtypes.EventDataPoint) ([]eventtypes.EventDataPoint, int64) {
	var currentBatchSize int64
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
	// failed events move to the backlog (outside the buffer) and
	// must not keep inflating the size-based sync trigger.
	s.currentSize.Add(-currentBatchSize)
	return events, currentBatchSize
}

// doSync performs the actual synchronization with chunking and error
// handling. ctx bounds every network call: past it nothing more is sent
// and what was not acknowledged goes to the backlog.
func (s *EventSyncStrategy) doSync(ctx context.Context) error {
	if s.backlog != nil {
		return s.doSyncDurable(ctx)
	}

	var events []eventtypes.EventDataPoint

	// First handle any previously failed events
	if retryBacklog := s.failedEvents.Sync(); len(retryBacklog) > 0 {
		s.logger.Info().
			Int("count", len(retryBacklog)).
			Msg("Processing previously failed events")
		events = append(events, retryBacklog...)
	}

	events, currentBatchSize := s.collectBatch(events)
	if len(events) == 0 {
		return nil
	}

	err := s.sendWithRetry(ctx, events)
	if err == nil {
		s.recordSynced(len(events), currentBatchSize)
		return nil
	}
	if !s.keepAfterFailure(ctx, events, err) {
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

// doSyncDurable is doSync with the on-disk backlog: queued batches are
// replayed first, in order, then the live batch is sent. While the intake
// does not answer, everything held in memory is moved to disk so that a
// restart loses nothing.
func (s *EventSyncStrategy) doSyncDurable(ctx context.Context) error {
	s.backlog.sweep()

	replayed, err := s.backlog.drain(ctx, replayFilesPerSync, s.replayBatch)
	if replayed > 0 {
		s.logger.Info().Int("events", replayed).Msg("Replayed events from the backlog")
		agentstate.RecordExportSuccess("event")
	}
	if err != nil {
		s.recordSendFailure(err)
		s.handOff(ctx, nil)
		return fmt.Errorf("replaying the event backlog: %w", err)
	}

	s.persistMu.Lock()
	events, size := s.collectBatch(nil)
	s.inflight = events
	s.persistMu.Unlock()
	if len(events) == 0 {
		return nil
	}
	err = s.sendWithRetry(ctx, events)
	if err == nil {
		s.clearInflight()
		s.recordSynced(len(events), size)
		return nil
	}
	if !s.keepAfterFailure(ctx, events, err) {
		s.clearInflight()
		return nil
	}
	s.handOff(ctx, events)
	return fmt.Errorf("failed to sync events after %d attempts: %w", s.retryAttempts, err)
}

// sendWithRetry sends one batch with the short in-tick retry. RetryIf
// keeps it for a transport blip but skips it entirely for a payload the
// intake refused on its merits: three attempts and two seconds of sleep
// inside the scheduler tick buy nothing when the same bytes get the same
// rejection.
func (s *EventSyncStrategy) sendWithRetry(ctx context.Context, events []eventtypes.EventDataPoint) error {
	return retry.Do(
		func() error {
			return s.sendEvents(ctx, events)
		},
		retry.Context(ctx),
		retry.Attempts(s.retryAttempts),
		retry.Delay(s.retryDelay),
		retry.DelayType(s.retryDelayFor),
		retry.RetryIf(exporterrors.IsRetryable),
		retry.OnRetry(func(n uint, err error) {
			s.logger.Warn().
				Err(err).
				Uint("attempt", n+1).
				Int("events_count", len(events)).
				Msg("Retrying event sync")
		}),
	)
}

func (s *EventSyncStrategy) recordSendFailure(err error) {
	agentstate.IncrementExportSendFailed("event", exporterrors.Reason(err))
	agentstate.RecordExportFailure("event", err.Error())
}

func (s *EventSyncStrategy) recordSynced(count int, size int64) {
	s.logger.Debug().
		Int("events_sent", count).
		Int64("batch_size_bytes", size).
		Msg("Successfully synced events")
	agentstate.RecordExportSuccess("event")
}

// keepAfterFailure records a failed send and says whether the batch must
// be kept. A batch the intake refused on its merits is dropped and counted:
// kept, it would pin the head of the backlog forever, so the backlog never
// drains and every tick re-sends bytes the intake already refused.
func (s *EventSyncStrategy) keepAfterFailure(ctx context.Context, events []eventtypes.EventDataPoint, err error) bool {
	s.recordSendFailure(err)
	if ctx.Err() != nil || exporterrors.IsRetryable(err) {
		return true
	}
	s.logger.Warn().
		Err(err).
		Int("dropped_events", len(events)).
		Msg("unrecoverable error from intake; discarding events (no retry)")
	agentstate.IncrementPushBufferDropped("event", len(events))
	return false
}

// replayBatch sends one queued batch once. The next tick is the retry, long
// after the first-resend delay the intake needs to see the ids it stored.
func (s *EventSyncStrategy) replayBatch(ctx context.Context, events []eventtypes.EventDataPoint) error {
	err := s.sendEvents(ctx, events)
	if err == nil {
		return nil
	}
	if ctx.Err() == nil && !exporterrors.IsRetryable(err) {
		s.recordSendFailure(err)
		s.logger.Warn().Err(err).Int("dropped_events", len(events)).
			Msg("unrecoverable error from intake; discarding a queued batch (no retry)")
		return errBatchRefused
	}
	return err
}

// persist puts a batch on disk. A write failure loses the batch, counted,
// with at most one warning a minute.
func (s *EventSyncStrategy) persist(events []eventtypes.EventDataPoint, durable bool) {
	if len(events) == 0 || s.backlog == nil {
		return
	}
	if err := s.backlog.append(events, durable); err != nil {
		agentstate.IncrementPushBufferDropped("event", len(events))
		s.unwarned.Add(int64(len(events)))
		now := time.Now().UnixNano()
		last := s.lastWarnAt.Load()
		if now-last >= int64(backlogWarnEvery) && s.lastWarnAt.CompareAndSwap(last, now) {
			s.logger.Warn().Err(err).Int64("dropped_events", s.unwarned.Swap(0)).
				Msg("Event backlog could not be written; events dropped")
		}
	}
}

func (s *EventSyncStrategy) clearInflight() {
	s.persistMu.Lock()
	s.inflight = nil
	s.persistMu.Unlock()
}

// handOff is how a sync gives the backlog the batch it could not deliver,
// then the buffer. After Shutdown has taken over it writes nothing: the
// batch is still in inflight and Shutdown puts it on disk, in order.
func (s *EventSyncStrategy) handOff(ctx context.Context, events []eventtypes.EventDataPoint) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if s.closing {
		return
	}
	// Past the deadline the write must be quick: no fsync (see append).
	durable := ctx.Err() == nil
	s.persist(events, durable)
	s.inflight = nil
	s.parkBuffer(durable)
}

// takeOver is the one write Shutdown makes: the batch an interrupted sync
// still holds, then the buffer. It also bars every later write of a sync.
func (s *EventSyncStrategy) takeOver() {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.closing = true
	s.persist(s.inflight, false)
	s.inflight = nil
	s.parkBuffer(false)
}

// parkBuffer moves what the in-memory buffer holds onto disk. Bounded by
// the buffer capacity, so a producer faster than the disk cannot hold it.
// The caller holds persistMu. Without a backlog it does nothing.
func (s *EventSyncStrategy) parkBuffer(durable bool) {
	if s.backlog == nil {
		return
	}
	for i := 0; i <= cap(s.buffer)/max(s.syncTriggerSize, 1)+1; i++ {
		events, _ := s.collectBatch(nil)
		if len(events) == 0 {
			return
		}
		s.persist(events, durable)
	}
}

// sendEvents sends a batch of events to the server
func (s *EventSyncStrategy) sendEvents(ctx context.Context, events []eventtypes.EventDataPoint) error {
	if len(events) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return exporterrors.Transport("sending events", err)
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

	var response *http.Response
	if poster, ok := s.server.(server.ContextStreamPoster); ok {
		response, err = poster.PostStreamContext(ctx, "/event/insert", string(eventsJSON))
	} else {
		response, err = postStreamAbandonable(ctx, s.server, string(eventsJSON))
	}
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

	s.logger.Debug().
		Int("status_code", response.StatusCode).
		Int("event_count", len(events)).
		Msg("Server confirmed receipt of events")

	return nil
}

// Start initializes and starts the sync strategy
func (s *EventSyncStrategy) Start(ctx context.Context) error {
	agentstate.SeedExportSendFailed("event")
	if ctx == nil {
		ctx = context.Background()
	}
	s.openBacklog()
	s.tickerOnce.Do(func() {
		s.ticker = time.NewTicker(s.config.SyncInterval)
		s.tickerStop = make(chan struct{})
		// Agent-wide cancellation stops the ticker goroutine even when
		// nothing calls Shutdown; stopOnce keeps the two paths from
		// double-closing.
		context.AfterFunc(ctx, func() {
			s.stopOnce.Do(func() { close(s.tickerStop) })
			s.syncCancel()
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

	// Wait for ongoing sync to complete. At the deadline it is cancelled
	// and Shutdown writes what it holds, itself and in order; the sync
	// writes nothing from then on.
	for s.syncInProgress.Load() {
		select {
		case <-ctx.Done():
			s.syncCancel()
			s.takeOver()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			s.logger.Debug().Msg("Waiting for ongoing sync to complete")
		}
	}

	// Final sync of remaining events, until the buffer and the backlog
	// are empty or ctx ends. Whatever is left is on disk.
	var syncErr error
	for ctx.Err() == nil {
		before := len(s.buffer) + s.backlogFiles()
		if syncErr = s.doSync(ctx); syncErr != nil {
			break
		}
		if after := len(s.buffer) + s.backlogFiles(); after == 0 || after >= before {
			break
		}
	}
	s.takeOver()
	s.syncCancel()
	if syncErr != nil {
		return syncErr
	}
	return nil
}

// retryDelayFor is the wait before the next attempt, n being the number of
// attempts already made. Later retries keep retry-go's default backoff.
func (s *EventSyncStrategy) retryDelayFor(n uint, err error, c *retry.Config) time.Duration {
	if n <= 1 {
		if s.firstRetryJitter <= 0 {
			return s.firstRetryDelay
		}
		return s.firstRetryDelay + mrand.N(s.firstRetryJitter)
	}
	return retry.CombineDelay(retry.BackOffDelay, retry.RandomDelay)(n, err, c)
}

// postStreamAbandonable runs a PostStream that cannot take a context so
// that cancelling ctx returns at once. The call itself ends on the client
// timeout; if it answers late its response is closed unread. The intake
// may have stored the batch, which the event ids absorb on the resend.
func postStreamAbandonable(ctx context.Context, srv server.Server, body string) (*http.Response, error) {
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := srv.PostStream("/event/insert", body)
		done <- result{resp, err}
	}()
	select {
	case r := <-done:
		return r.resp, r.err
	case <-ctx.Done():
		go func() {
			if r := <-done; r.resp != nil && r.resp.Body != nil {
				_ = r.resp.Body.Close()
			}
		}()
		return nil, ctx.Err()
	}
}
