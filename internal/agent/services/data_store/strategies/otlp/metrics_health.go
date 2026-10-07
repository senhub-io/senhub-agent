package otlp

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// metricsHealth records how the last metric exports ended, so the stop can
// tell a collector that is down from one that is healthy. The logs pipeline
// keeps the same signal in persistentLogExporter.
type metricsHealth struct {
	lastOKNs   atomic.Int64
	lastFailNs atomic.Int64
	now        func() time.Time
}

func (h *metricsHealth) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

func (h *metricsHealth) markSuccess() { h.lastOKNs.Store(h.clock().UnixNano()) }
func (h *metricsHealth) markFailure() { h.lastFailNs.Store(h.clock().UnixNano()) }

// silentIntervals is how many push intervals without an acknowledged export
// make the backend count as silent.
const silentIntervals = 3

// backendDown reports whether the collector is known to be down (the last
// export failed and none succeeded since) or silent (it acknowledged
// nothing for silentIntervals pushes). A collector never tried is not down.
func (h *metricsHealth) backendDown(interval time.Duration) bool {
	ok, fail := h.lastOKNs.Load(), h.lastFailNs.Load()
	if fail > ok {
		return true
	}
	return ok != 0 && interval > 0 && h.clock().UnixNano()-ok > int64(silentIntervals)*int64(interval)
}

// reportMetricsFinalFlush says what became of the last metrics point at
// stop and returns the error the caller should propagate. Against a
// collector known to be down, running out of time is the expected outcome,
// not a failure: one Info line. A deadline or a failure while the collector
// was healthy stays a warning.
func reportMetricsFinalFlush(log *logger.ModuleLogger, backendDown bool, seriesBefore, seriesAfter int, budget time.Duration, ctxErr, err error) error {
	if log == nil {
		return err
	}
	deadlineOnly := err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
	if backendDown && deadlineOnly {
		if seriesBefore > 0 || err != nil {
			log.Info().Int("series_in_store", seriesAfter).
				Msg("final metrics point not delivered: collector unreachable")
		}
		return nil
	}
	if ctxErr != nil {
		log.Warn().
			Dur("flush_budget", budget).
			Int("series_in_store_before_flush", seriesBefore).
			Int("series_in_store_after_flush", seriesAfter).
			Msg("OTLP final flush did not finish within its budget: the collector did not answer in time, and what was still queued was dropped")
	}
	if err != nil {
		log.Warn().Err(err).Msg("OTLP strategy shutdown encountered errors")
		return err
	}
	return nil
}
