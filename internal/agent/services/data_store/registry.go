package data_store

import (
	"fmt"
	"sort"
	"sync"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
)

// StrategyDeps carries what a strategy may need beyond its own
// configuration. It exists so adding a dependency does not change the
// signature of every factory: a strategy takes what it uses and ignores
// the rest.
type StrategyDeps struct {
	AgentConfig configuration.AgentConfiguration
	Logger      *logger.Logger
	// Registry resolves a metric's display name and unit from the probe's
	// definition. The PRTG push path needs it to name channels the way the
	// pull endpoint does (#293).
	Registry *transformers.TransformerRegistry
}

// StrategyFactory builds one strategy instance. It returns an error
// rather than a nil interface: a constructor that could not honour its
// configuration has a reason, and the caller records it as the failure
// state an operator can see (#826).
type StrategyFactory func(params configuration.StorageConfigParams, deps StrategyDeps) (SyncStrategy, error)

var (
	factoriesMu sync.RWMutex
	factories   = map[string]StrategyFactory{}
)

// RegisterStrategy makes a strategy type available to the data store.
//
// Each strategy package registers itself, so the hub no longer imports
// them: adding a sink stops meaning "edit a switch in data_store.go",
// and an edition that ships a different set of sinks does not need a
// different hub. Same pattern the probe registry already uses.
//
// Registration happens at package init, which is the one place the house
// rule against work in init() makes an exception for: it records a
// function pointer and does nothing else.
func RegisterStrategy(name string, factory StrategyFactory) {
	if name == "" || factory == nil {
		panic("data_store: strategy registered with no name or no factory")
	}
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	if _, exists := factories[name]; exists {
		panic(fmt.Sprintf("data_store: strategy %q registered twice", name))
	}
	factories[name] = factory
}

// lookupStrategyFactory returns the factory for a strategy type.
func lookupStrategyFactory(name string) (StrategyFactory, bool) {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	f, ok := factories[name]
	return f, ok
}

// RegisteredStrategyNames lists the strategy types compiled into this
// build, sorted. Used by diagnostics and by the tests that assert the
// shipped set.
func RegisteredStrategyNames() []string {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LiveUpdatable is implemented by a strategy that can absorb a
// configuration change without being torn down and rebuilt.
//
// The hub used to type-assert one concrete strategy here, which made the
// data store import a sink package to ask it a question any sink could
// answer. Stating the capability as an interface lets a strategy opt in
// by implementing the method, and keeps the hub ignorant of who is on
// the other side.
//
// Returning an error means "I could not apply this" — the caller then
// falls back to a full recreate, which is always available.
type LiveUpdatable interface {
	UpdateConfiguration(params map[string]interface{}) error
}
