// Package exporthook is the public face of the export-path extension
// point (senhub-agent.go/internal/agent/services/exporthook), for a
// build that adds its own observers and self-records from outside the
// core module.
package exporthook

import iexporthook "senhub-agent.go/internal/agent/services/exporthook"

type (
	SeriesPoint        = iexporthook.SeriesPoint
	SeriesObserver     = iexporthook.SeriesObserver
	SelfRecord         = iexporthook.SelfRecord
	SelfRecordProvider = iexporthook.SelfRecordProvider
)

// RegisterSeriesObserver adds an observer of exported series.
func RegisterSeriesObserver(o SeriesObserver) { iexporthook.RegisterSeriesObserver(o) }

// RegisterSelfRecordProvider adds a provider of agent self-records.
func RegisterSelfRecordProvider(p SelfRecordProvider) { iexporthook.RegisterSelfRecordProvider(p) }

// Authorized reports whether the agent's licence allows feature.
func Authorized(feature string) bool { return iexporthook.Authorized(feature) }
