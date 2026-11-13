// Package subcollect keeps the log volume of an optional read proportional
// to what changes, not to how often the probe runs.
//
// A probe that cannot read one of its sub-collections drops the matching
// metrics for that cycle. Logging every cycle floods the log of a probe
// polling each minute; logging nothing leaves a missing series with no
// explanation. A Tracker logs the transition into failure at the level the
// caller chose, stays at Debug while the failure persists, and reports the
// recovery once.
package subcollect

import (
	"sync"

	"senhub-agent.go/internal/agent/services/logger"
)

// Level is the severity used when a sub-collection starts failing.
type Level int

const (
	// Warn is for a failure an operator should act on.
	Warn Level = iota
	// Debug is for an absence that is expected on some targets.
	Debug
)

// Tracker remembers which sub-collections are currently failing. The zero
// value is ready to use and safe for concurrent use.
type Tracker struct {
	mu      sync.Mutex
	failing map[string]struct{}
}

// Failed records a failed read of the sub-collection named key. The first
// failure after a success (or the first ever) is logged at level; further
// consecutive failures are logged at Debug.
func (t *Tracker) Failed(log *logger.ModuleLogger, level Level, key string, err error, msg string) {
	t.mu.Lock()
	if t.failing == nil {
		t.failing = make(map[string]struct{})
	}
	_, already := t.failing[key]
	t.failing[key] = struct{}{}
	t.mu.Unlock()

	if already || level == Debug {
		log.Debug().Err(err).Str("sub_collection", key).Msg(msg)
		return
	}
	log.Warn().Err(err).Str("sub_collection", key).Msg(msg)
}

// Succeeded records a successful read. It logs once, at Info, when the
// sub-collection was failing before.
func (t *Tracker) Succeeded(log *logger.ModuleLogger, key string, msg string) {
	t.mu.Lock()
	_, was := t.failing[key]
	delete(t.failing, key)
	t.mu.Unlock()

	if was {
		log.Info().Str("sub_collection", key).Msg(msg)
	}
}
