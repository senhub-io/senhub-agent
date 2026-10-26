package snmppoll

import (
	"errors"
	"strings"
	"time"
)

const (
	// minSweepBudget is the least wall-clock a topology sweep may take
	// before its remaining walks are skipped; the budget is otherwise the
	// poll interval, so a sweep does not hold the cycle past one interval.
	minSweepBudget = 30 * time.Second
	// maxSweepRetryDelay caps the wait between two sweeps of a device
	// whose sweeps fail, so a device that comes back is swept again, and
	// its metrics resume, within that delay.
	maxSweepRetryDelay = 5 * time.Minute
)

var (
	errSweepSilent = errors.New("device did not answer earlier in this sweep")
	errSweepBudget = errors.New("topology sweep budget spent")
)

// sweepClient bounds one topology sweep. Each read is otherwise bounded only
// by the per-request timeout and retries, so against a device that answers
// nothing a sweep took the sum of a dozen such waits, inside the probe's
// collection. Once a read has timed out with nothing answered before it,
// every later read fails at once; once the budget is spent, later walks
// fail at once. Scalar reads stay allowed past the budget: the identity is
// read last and a sweep without it is a failed one.
type sweepClient struct {
	snmpClient
	deadline time.Time
	now      func() time.Time
	answered bool
	silent   bool
	cut      bool
}

func newSweepClient(c snmpClient, budget time.Duration, now func() time.Time) *sweepClient {
	return &sweepClient{snmpClient: c, deadline: now().Add(budget), now: now}
}

func (c *sweepClient) Get(oids []string) ([]snmpVarBind, error) {
	if c.silent {
		return nil, errSweepSilent
	}
	binds, err := c.snmpClient.Get(oids)
	c.record(err)
	return binds, err
}

func (c *sweepClient) BulkWalk(base string) ([]snmpVarBind, error) {
	if err := c.walkAllowed(); err != nil {
		return nil, err
	}
	binds, err := c.snmpClient.BulkWalk(base)
	c.record(err)
	return binds, err
}

func (c *sweepClient) WalkRaw(base string) ([]snmpRawBind, error) {
	if err := c.walkAllowed(); err != nil {
		return nil, err
	}
	binds, err := c.snmpClient.WalkRaw(base)
	c.record(err)
	return binds, err
}

func (c *sweepClient) walkAllowed() error {
	if c.silent {
		return errSweepSilent
	}
	if !c.now().Before(c.deadline) {
		c.cut = true
		return errSweepBudget
	}
	return nil
}

func (c *sweepClient) record(err error) {
	if err == nil {
		c.answered = true
		return
	}
	if !c.answered && isTimeout(err) {
		c.silent = true
	}
}

// isTimeout matches gosnmp's "request timeout (after N retries)" and a
// transport "i/o timeout".
func isTimeout(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}

// sweepBudget is the poll interval, never under minSweepBudget.
func sweepBudget(cfg *config) time.Duration {
	if cfg.Interval > minSweepBudget {
		return cfg.Interval
	}
	return minSweepBudget
}

// sweepRetryDelay is the wait after the n-th consecutive failed sweep: the
// next cycle after the first failure, then twice as long each time, up to
// maxSweepRetryDelay, or one cycle when the poll interval is longer.
func sweepRetryDelay(pollInterval time.Duration, failures int) time.Duration {
	d := pollInterval
	for i := 1; i < failures && d < maxSweepRetryDelay; i++ {
		d *= 2
	}
	if d > maxSweepRetryDelay && pollInterval < maxSweepRetryDelay {
		d = maxSweepRetryDelay
	}
	return d
}
