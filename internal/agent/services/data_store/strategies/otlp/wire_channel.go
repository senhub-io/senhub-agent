package otlp

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/exporterrors"
	"senhub-agent.go/internal/agent/services/logger"
)

// errExportQueued says the request did not reach the backend in this call
// and is on disk for replay. It is not a new failure: callers keep their
// success accounting out of it and their error accounting off it.
var errExportQueued = errors.New("export kept on disk for replay")

// errWireBackendDown is returned when a request could not be exported (the
// backend is known down) and could not be written to disk either.
var errWireBackendDown = errors.New("otlp backend down and the queue did not take the request")

// wireProbeTimeout bounds the live attempt made while the backend is known
// down. Without it a collector that drops packets holds the pushing
// goroutine for the whole export timeout at every probe, and the requests
// that should have been written meanwhile are never produced.
const wireProbeTimeout = 4 * time.Second

// wireOp is one export request, as the channel needs to see it.
type wireOp interface {
	// units is how many data points or spans the request carries.
	units() int
	// send makes the live export attempt.
	send(ctx context.Context) error
	// payload returns the serialized request. attempted says whether send
	// ran: when it did not, the request still has to be encoded.
	payload(ctx context.Context, attempted bool) ([]byte, error)
}

// wireChannel keeps the failed export requests of one signal (metrics or
// traces) on disk and tracks whether the backend answers: the metrics and
// traces counterpart of persistentLogExporter. It does not know how a
// request is built or sent; the signal's exporter decorator hands each export
// over as a wireOp and gives the channel the function that replays a stored
// request.
type wireChannel struct {
	exporterHealth
	signal string
	queue  *wireQueue
	// ordered keeps live requests behind a backlog: while anything waits on
	// disk, a new request is appended to it and the replay delivers
	// everything in order. Metrics need it, because a time-series store that
	// refuses samples older than the newest one it holds would drop the
	// backlog if live data overtook it. Traces do not care.
	ordered bool
	// countErrors makes the channel count failed exports itself. The
	// metrics push counts its own.
	countErrors bool
	reporter    *partialSuccessReporter
	logger      *logger.ModuleLogger

	// replaySend sends one stored request through the exporter.
	replaySend func(ctx context.Context, request []byte) error

	lostUnits atomic.Int64
}

func newWireChannel(signal string, queue *wireQueue, ordered, countErrors bool, log *logger.ModuleLogger) *wireChannel {
	c := &wireChannel{
		signal:      signal,
		queue:       queue,
		ordered:     ordered,
		countErrors: countErrors,
		reporter:    newPartialSuccessReporter(log, "export"),
		logger:      log,
	}
	c.exporterHealth.init("otlp/"+signal, log)
	return c
}

// export runs one export through the queue: delivered, or kept on disk.
func (c *wireChannel) export(ctx context.Context, op wireOp) error {
	attempt := true
	switch {
	case c.stopping.Load():
		if !c.knownUp() {
			attempt = false
		} else {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(c.drainTimeout.Load()))
			defer cancel()
		}
	case c.ordered && c.queue.hasFiles():
		attempt = false
	case c.backendDown():
		if !c.claimAttempt() {
			attempt = false
		} else {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, wireProbeTimeout)
			defer cancel()
		}
	}

	if !attempt {
		if !c.persist(ctx, op, false) {
			return errWireBackendDown
		}
		return errExportQueued
	}

	if !c.stopping.Load() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-c.stopCh:
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	err := c.reporter.reportRejections(op.send(ctx))
	if err == nil {
		c.markSuccess()
		return nil
	}

	if c.countErrors {
		agentstate.IncrementOTLPExportErrors(c.signal)
		agentstate.RecordExportFailure("otlp/"+c.signal, redactSensitive(err.Error()))
	}
	if !exporterrors.IsRetryable(classifyExportError(err)) {
		// The receiver refused the payload on its merits: queueing it would
		// replay the same refusal at every recovery and evict requests that
		// could still be delivered (#833).
		agentstate.AddOTLPDropped("receiver_rejected", op.units())
		c.warnThrottled(func(ev *zerolog.Event) {
			ev.Str("error", redactSensitive(err.Error())).Int("units", op.units()).
				Msg("OTLP " + c.signal + " export rejected by the receiver; discarded (not queued: a retry gets the same rejection)")
		})
		return err
	}

	c.markFailure()
	if c.persist(ctx, op, true) {
		c.warnThrottled(func(ev *zerolog.Event) {
			ev.Str("error", redactSensitive(err.Error())).Int("units", op.units()).
				Msg("OTLP " + c.signal + " export failed; request kept on disk for replay")
		})
	}
	return err
}

// persist serialises the request into the queue. Returns false when it could
// not be written.
func (c *wireChannel) persist(ctx context.Context, op wireOp, attempted bool) bool {
	if op.units() == 0 {
		return true
	}
	request, err := op.payload(ctx, attempted)
	if err == nil {
		err = c.queue.enqueue(request, op.units())
	}
	if err != nil {
		if c.logger != nil {
			c.logger.Warn().Err(err).Int("units", op.units()).
				Msg("OTLP " + c.signal + " queue: could not keep the request; its data is lost")
		}
		c.lostUnits.Add(int64(op.units()))
		return false
	}
	c.fireQueued()
	return true
}

// persistRequest keeps a request that is already serialized, for the relays,
// which forward what an application sent rather than what an SDK built.
func (c *wireChannel) persistRequest(request []byte, units int) bool {
	if units == 0 {
		return true
	}
	if err := c.queue.enqueue(request, units); err != nil {
		if c.logger != nil {
			c.logger.Warn().Err(err).Int("units", units).
				Msg("OTLP " + c.signal + " queue: could not keep the request; its data is lost")
		}
		c.lostUnits.Add(int64(units))
		return false
	}
	c.fireQueued()
	return true
}

// relayDecision says what a relay does with the batch it is about to send.
type relayDecision int

const (
	relaySend relayDecision = iota
	relayKeep
)

// relayAdmit is export's gate for a caller that sends on its own: it reports
// whether to try the network, with the same rules.
func (c *wireChannel) relayAdmit() relayDecision {
	switch {
	case c.stopping.Load():
		if c.knownUp() {
			return relaySend
		}
		return relayKeep
	case c.ordered && c.queue.hasFiles():
		return relayKeep
	case c.backendDown() && !c.claimAttempt():
		return relayKeep
	}
	return relaySend
}

// relayResult applies the outcome of a relay's own send. It returns true when
// the batch was kept on disk and so is not lost.
func (c *wireChannel) relayResult(err error, request func() ([]byte, error), units int) bool {
	err = c.reporter.reportRejections(err)
	if err == nil {
		c.markSuccess()
		return false
	}
	if !exporterrors.IsRetryable(classifyExportError(err)) {
		agentstate.AddOTLPDropped("receiver_rejected", units)
		return false
	}
	c.markFailure()
	return c.keepRelayed(request, units)
}

// keepRelayed serialises and queues a relayed batch.
func (c *wireChannel) keepRelayed(request func() ([]byte, error), units int) bool {
	b, err := request()
	if err != nil {
		c.lostUnits.Add(int64(units))
		return false
	}
	return c.persistRequest(b, units)
}

// exportDirect replays one stored request and reports the outcome; a failure
// is never persisted a second time. A cancelled ctx (the agent is stopping)
// is not a verdict on the backend.
func (c *wireChannel) exportDirect(ctx context.Context, request []byte, units int) error {
	err := c.reporter.reportRejections(c.replaySend(ctx, request))
	if err == nil {
		c.markSuccess()
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	if !exporterrors.IsRetryable(classifyExportError(err)) {
		agentstate.AddOTLPDropped("receiver_rejected", units)
		if c.logger != nil {
			c.logger.Warn().Str("error", redactSensitive(err.Error())).Int("units", units).
				Msg("OTLP " + c.signal + " queue: the receiver refused a queued request; discarded")
		}
		return errBatchDiscarded
	}
	c.markFailure()
	return err
}

// wireReplayer drains the queue to the backend, one request at a time,
// deleting a file only after the export was acknowledged. Concurrent drains
// (boot replay, recovery, retry loop) collapse to one.
type wireReplayer struct {
	replayLoop
	ch            *wireChannel
	running       atomic.Bool
	exportTimeout time.Duration
}

func newWireReplayer(ch *wireChannel, log *logger.ModuleLogger) *wireReplayer {
	r := &wireReplayer{ch: ch, exportTimeout: replayExportTimeout}
	r.initLoop("OTLP "+ch.signal+" queue", log)
	r.pending = ch.queue.pending
	r.run = r.replay
	r.sweep = func() { ch.queue.sweepAged() }
	r.afterStop = ch.closeRecovery
	return r
}

// replay drains the queue and returns the units acknowledged by the backend.
// On the first failed export it stops and returns the error: that file and
// the ones after it stay on disk. Requests queued while it runs are picked
// up by the next pass, so a backlog drains to empty and live data resumes
// behind it, in order.
func (r *wireReplayer) replay() (int, error) {
	if !r.running.CompareAndSwap(false, true) {
		return 0, nil
	}
	defer r.running.Store(false)

	r.ch.queue.sweepAged()
	total := 0
	var err error
	for pass := 0; pass < 64 && r.ch.queue.hasFiles(); pass++ {
		var n int
		n, err = r.ch.queue.drainAcked(r.sendRequest)
		total += n
		if err != nil || r.ctx.Err() != nil {
			break
		}
	}
	if total > 0 && r.logger != nil {
		r.logger.Info().Int("units", total).Msg("OTLP " + r.ch.signal + " queue: replayed requests")
	}
	if err != nil && r.logger != nil && r.ctx.Err() == nil {
		r.logger.Warn().Err(err).Int("replayed", total).
			Msg("OTLP " + r.ch.signal + " queue: replay stopped, the backend did not acknowledge; remaining requests stay on disk")
	}
	return total, err
}

func (r *wireReplayer) sendRequest(request []byte, units int) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.exportTimeout)
	defer cancel()
	return r.ch.exportDirect(ctx, request, units)
}

// ── Metrics ──────────────────────────────────────────────────────────

// countMetricData totals the data points of an SDK metric batch.
func countMetricData(rm *metricdata.ResourceMetrics) int {
	if rm == nil {
		return 0
	}
	n := 0
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Gauge[float64]:
				n += len(d.DataPoints)
			case metricdata.Gauge[int64]:
				n += len(d.DataPoints)
			case metricdata.Sum[float64]:
				n += len(d.DataPoints)
			case metricdata.Sum[int64]:
				n += len(d.DataPoints)
			case metricdata.Histogram[float64]:
				n += len(d.DataPoints)
			case metricdata.Histogram[int64]:
				n += len(d.DataPoints)
			case metricdata.ExponentialHistogram[float64]:
				n += len(d.DataPoints)
			case metricdata.ExponentialHistogram[int64]:
				n += len(d.DataPoints)
			case metricdata.Summary:
				n += len(d.DataPoints)
			}
		}
	}
	return n
}
