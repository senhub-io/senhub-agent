// Package exporthook lets a build add work to the metric export path
// without the core knowing what that work is. An observer sees every
// series an output exports; a provider adds records to the agent's own
// metrics; the authorizer tells either whether the licence allows a
// feature. With nothing registered, the export path pays one read lock.
package exporthook

import (
	"sync"
	"time"
)

// SeriesPoint is one exported series: its metric name and the full set
// of per-point attributes. Resource attributes are common to the agent
// and are not part of it.
type SeriesPoint struct {
	Probe      string
	Name       string
	Attributes map[string]string
}

// SeriesObserver receives the series an output is about to export. It
// runs on the export path and must return quickly; it must not keep or
// modify the Attributes maps.
type SeriesObserver func(output string, points []SeriesPoint, now time.Time)

// SelfRecord is one record a provider adds to the agent's own metrics.
// Type is "gauge" or "counter".
type SelfRecord struct {
	Name        string
	Unit        string
	Type        string
	Description string
	Value       float64
	Attributes  map[string]string
}

// SelfRecordProvider returns the records to add at one export.
type SelfRecordProvider func(now time.Time) []SelfRecord

// Authorizer reports whether the licence the agent runs under allows a
// named feature.
type Authorizer func(feature string) bool

var (
	mu         sync.RWMutex
	observers  []SeriesObserver
	providers  []SelfRecordProvider
	authorizer Authorizer
)

// RegisterSeriesObserver adds an observer of exported series.
func RegisterSeriesObserver(o SeriesObserver) {
	if o == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	observers = append(observers, o)
}

// RegisterSelfRecordProvider adds a provider of agent self-records.
func RegisterSelfRecordProvider(p SelfRecordProvider) {
	if p == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	providers = append(providers, p)
}

// SetAuthorizer installs the licence check. The core replaces it each
// time the licence changes.
func SetAuthorizer(a Authorizer) {
	mu.Lock()
	defer mu.Unlock()
	authorizer = a
}

// Authorized reports whether the licence allows feature. Without an
// authorizer nothing is allowed.
func Authorized(feature string) bool {
	mu.RLock()
	a := authorizer
	mu.RUnlock()
	return a != nil && a(feature)
}

// HasSeriesObservers reports whether building SeriesPoints is worth it.
func HasSeriesObservers() bool {
	mu.RLock()
	defer mu.RUnlock()
	return len(observers) > 0
}

// ObserveSeries hands points to every registered observer.
func ObserveSeries(output string, points []SeriesPoint, now time.Time) {
	mu.RLock()
	obs := observers
	mu.RUnlock()
	for _, o := range obs {
		o(output, points, now)
	}
}

// SelfRecords collects the records of every registered provider.
func SelfRecords(now time.Time) []SelfRecord {
	mu.RLock()
	ps := providers
	mu.RUnlock()
	var out []SelfRecord
	for _, p := range ps {
		out = append(out, p(now)...)
	}
	return out
}

// reset clears every registration; tests only.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	observers, providers, authorizer = nil, nil, nil
}
