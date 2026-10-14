//go:build windows

package cpu

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/windows/pdh"
)

const (
	windowsTimeOffsetCounter      = `\Windows Time Service\Computed Time Offset`
	windowsTimeSourceCountCounter = `\Windows Time Service\NTP Client Time Source Count`
)

type windowsClockSync struct {
	query *pdh.Query
	// readCounters is injectable so the pure conversion below can be tested
	// without a real PDH query.
	readCounters func(q *pdh.Query) (offsetMicros float64, sourceCount float64, err error)
}

func newClockSync() func() (clockSync, error) {
	s := &windowsClockSync{
		readCounters: readWindowsTimeCounters,
	}
	return s.read
}

func (s *windowsClockSync) read() (clockSync, error) {
	q, err := s.ensureQuery()
	if err != nil {
		return clockSync{}, err
	}

	offsetMicros, sourceCount, err := s.readCounters(q)
	if err != nil {
		s.closeQuery()
		return clockSync{}, err
	}

	return clockSyncFromCounters(offsetMicros, sourceCount)
}

func (s *windowsClockSync) ensureQuery() (*pdh.Query, error) {
	if s.query != nil {
		return s.query, nil
	}

	q, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("create Windows Time Service PDH query: %w", err)
	}
	if err := q.AddCounter(windowsTimeOffsetCounter); err != nil {
		q.Close()
		return nil, fmt.Errorf("add Windows Time Service computed time offset counter: %w", err)
	}
	if err := q.AddCounter(windowsTimeSourceCountCounter); err != nil {
		q.Close()
		return nil, fmt.Errorf("add Windows Time Service NTP client time source count counter: %w", err)
	}

	s.query = q
	return q, nil
}

func (s *windowsClockSync) closeQuery() {
	if s.query != nil {
		s.query.Close()
		s.query = nil
	}
}

func readWindowsTimeCounters(q *pdh.Query) (float64, float64, error) {
	// Computed Time Offset is an instantaneous counter; one Collect is enough.
	if err := q.Collect(); err != nil {
		return 0, 0, fmt.Errorf("collect Windows Time Service counters: %w", err)
	}

	offsetMicros, err := q.GetCounterValue(windowsTimeOffsetCounter)
	if err != nil {
		return 0, 0, fmt.Errorf("read Windows Time Service computed time offset: %w", err)
	}
	sourceCount, err := q.GetCounterValue(windowsTimeSourceCountCounter)
	if err != nil {
		return 0, 0, fmt.Errorf("read Windows Time Service NTP client time source count: %w", err)
	}

	return offsetMicros, sourceCount, nil
}

func clockSyncFromCounters(offsetMicros float64, sourceCount float64) (clockSync, error) {
	return clockSync{
		synced:          sourceCount > 0,
		offset:          time.Duration(offsetMicros * float64(time.Microsecond)),
		maxErrorUnknown: true,
		source:          "w32time",
	}, nil
}
