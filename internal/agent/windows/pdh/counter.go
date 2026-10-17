package pdh

import (
	"errors"
	"fmt"
	"time"
)

// ErrNoData reports a PDH_NO_DATA condition. It is expected on the first
// sample of a rate counter — PDH has no delta until a second sample exists —
// so callers that prime then re-collect should tolerate it via errors.Is
// rather than treating it as a hard failure.
var ErrNoData = errors.New("no PDH sample available yet")

// PrimeInterval is the wait between the priming sample and the first real
// one, long enough for PDH to hold a delta for rate counters.
const PrimeInterval = time.Second

// CounterDef names a performance counter path and the instance it targets
// ("*" meaning every instance, to be expanded by the caller).
type CounterDef struct {
	Path     string
	Instance string
}

type sampler interface {
	Collect() error
}

// PrimeSamples takes the first sample of q and waits PrimeInterval so the
// next Collect has a delta to read. PDH_NO_DATA on that first sample is
// expected and ignored; any other error is real.
func PrimeSamples(q sampler) error {
	return primeSamples(q, time.Sleep)
}

func primeSamples(q sampler, sleep func(time.Duration)) error {
	if err := q.Collect(); err != nil && !errors.Is(err, ErrNoData) {
		return fmt.Errorf("failed initial sample collection: %w", err)
	}
	sleep(PrimeInterval)
	return nil
}
