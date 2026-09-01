package data_store

import (
	"fmt"
	"sort"
	"sync"

	"senhub-agent.go/internal/agent/services/configuration"
)

// A strategy reads the keys it knows out of a free-form map and ignores
// the rest, so a key nobody reads sits in the file looking like it does
// something. That cost an operator an hour over `insecure: true`, which
// this agent spells `tls: { enabled: false }`: the check said the
// configuration was valid, and the agent negotiated TLS anyway (#846).
//
// The declared set is what a strategy's source mentions ANYWHERE, not
// only at the top level, so nesting never produces a false accusation.
// What it catches is the key that appears nowhere in the strategy at
// all — a typo, a spelling borrowed from another tool, an option that
// was never implemented — which cannot be read by definition.
type knownParams struct {
	known  map[string]struct{}
	legacy map[string]string
}

var (
	knownParamsMu sync.RWMutex
	knownParamSet = map[string]knownParams{}
)

// RegisterKnownParams declares the parameter names a strategy reads,
// and the spellings it used to answer to mapped onto the current one.
//
// Called from the same package that registers the factories, so the
// declaration sits with the strategy and the hub stays ignorant of
// what any particular sink accepts.
func RegisterKnownParams(name string, known []string, legacy map[string]string) {
	if name == "" {
		panic("data_store: known params registered with no strategy name")
	}
	set := make(map[string]struct{}, len(known))
	for _, key := range known {
		set[key] = struct{}{}
	}
	for alias := range legacy {
		if _, declared := set[alias]; declared {
			panic(fmt.Sprintf("data_store: strategy %q declares %q as both read and legacy", name, alias))
		}
	}
	knownParamsMu.Lock()
	defer knownParamsMu.Unlock()
	knownParamSet[name] = knownParams{known: set, legacy: legacy}
}

// UnreadParam is a key present in a strategy's configuration that the
// strategy does not read. Replacement names the current spelling when
// there is one; empty means the key has no equivalent at all.
type UnreadParam struct {
	Key         string
	Replacement string
}

// UnreadParamsFor returns the configured keys the named strategy does
// not read, sorted. A strategy that declared nothing returns nothing:
// silence beats accusing every key of an undeclared sink.
func UnreadParamsFor(name string, params map[string]interface{}) []UnreadParam {
	knownParamsMu.RLock()
	declared, ok := knownParamSet[name]
	knownParamsMu.RUnlock()
	if !ok {
		return nil
	}

	var unread []UnreadParam
	for key := range params {
		if _, isKnown := declared.known[key]; isKnown {
			continue
		}
		unread = append(unread, UnreadParam{Key: key, Replacement: declared.legacy[key]})
	}
	sort.Slice(unread, func(i, j int) bool { return unread[i].Key < unread[j].Key })
	return unread
}

// StrategiesDeclaringParams lists the strategies that declared their
// parameter names, sorted. Used by the guard test that keeps the
// declarations in step with the code.
func StrategiesDeclaringParams() []string {
	knownParamsMu.RLock()
	defer knownParamsMu.RUnlock()
	names := make([]string, 0, len(knownParamSet))
	for name := range knownParamSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// KnownParamsFor returns the declared parameter names of a strategy,
// sorted. Empty when the strategy declared nothing.
func KnownParamsFor(name string) []string {
	knownParamsMu.RLock()
	defer knownParamsMu.RUnlock()
	declared, ok := knownParamSet[name]
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(declared.known))
	for key := range declared.known {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ParamValidator answers whether a strategy would accept a
// configuration, without building the strategy.
//
// `agent config check` reported a file as valid and the agent then
// refused the same file at construction, on validation the strategy
// already knew how to run — one of the messages even said
// "ValidateConfigParams will report it". The check simply never asked
// (#848).
type ParamValidator func(params configuration.StorageConfigParams) error

var (
	validatorsMu sync.RWMutex
	validators   = map[string]ParamValidator{}
)

// RegisterParamValidator declares how to validate a strategy's
// parameters without side effects. Registered next to the factory.
func RegisterParamValidator(name string, validate ParamValidator) {
	if name == "" || validate == nil {
		panic("data_store: param validator registered with no name or no function")
	}
	validatorsMu.Lock()
	defer validatorsMu.Unlock()
	validators[name] = validate
}

// ValidateStrategyParams runs the registered validator for a strategy.
// A strategy with no registered validator returns nil: not knowing how
// to check is not the same as finding a problem.
func ValidateStrategyParams(name string, params configuration.StorageConfigParams) error {
	validatorsMu.RLock()
	validate, ok := validators[name]
	validatorsMu.RUnlock()
	if !ok {
		return nil
	}
	return validate(params)
}
