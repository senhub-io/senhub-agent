// senhub-agent/internal/agent/services/data_store/strategies/http/http_lookups.go

package http

import (
	"embed"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/services/logger"
)

//go:embed lookups/lookups.yaml
var lookupsFile embed.FS

// LookupValue represents a single value in a lookup mapping
type LookupValue struct {
	Text        string `yaml:"text" json:"text"`
	Severity    string `yaml:"severity" json:"severity"` // "ok", "warning", "error", "unknown"
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// LookupDefinition defines a complete lookup table
type LookupDefinition struct {
	ID           string              `yaml:"-" json:"id"` // Populated from map key
	Description  string              `yaml:"description" json:"description"`
	Type         string              `yaml:"type" json:"type"` // "status", "health", "enum"
	Source       string              `yaml:"source" json:"source"`
	Mappings     map[int]LookupValue `yaml:"mappings" json:"mappings"`
	DesiredValue int                 `yaml:"desired_value" json:"desired_value"`
}

// LookupRegistry manages all lookup definitions
type LookupRegistry struct {
	lookups map[string]LookupDefinition
	mu      sync.RWMutex
	logger  *logger.ModuleLogger
}

// NewLookupRegistry creates and initializes a new lookup registry
func NewLookupRegistry(parentLogger *zerolog.Logger) (*LookupRegistry, error) {
	moduleLogger := logger.NewModuleLogger(parentLogger, "lookups")

	registry := &LookupRegistry{
		lookups: make(map[string]LookupDefinition),
		logger:  moduleLogger,
	}

	// Load lookups from embedded file
	if err := registry.loadLookups(); err != nil {
		return nil, fmt.Errorf("failed to load lookups: %w", err)
	}

	return registry, nil
}

// loadLookups loads lookup definitions from the embedded YAML file
func (r *LookupRegistry) loadLookups() error {
	// Read embedded lookups file
	data, err := lookupsFile.ReadFile("lookups/lookups.yaml")
	if err != nil {
		return fmt.Errorf("failed to read lookups.yaml: %w", err)
	}

	// Parse YAML
	var rawLookups map[string]LookupDefinition
	if err := yaml.Unmarshal(data, &rawLookups); err != nil {
		return fmt.Errorf("failed to parse lookups.yaml: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Store lookups with IDs populated from keys
	for id, lookup := range rawLookups {
		lookup.ID = id
		r.lookups[id] = lookup
		r.logger.Debug().
			Str("id", id).
			Int("mappings", len(lookup.Mappings)).
			Str("type", lookup.Type).
			Msg("Loaded lookup definition")
	}

	r.logger.Info().
		Int("count", len(r.lookups)).
		Msg("Lookup registry initialized")

	return nil
}

// GetLookup retrieves a lookup definition by ID
func (r *LookupRegistry) GetLookup(id string) (LookupDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lookup, exists := r.lookups[id]
	return lookup, exists
}

// GetAllLookups returns all lookup definitions
//
// NOTE: Returns a shallow copy of the lookups map to prevent external modifications.
// While LookupDefinition structs are copied by value, their internal Mappings maps
// are still references. This copy protects the registry's map structure itself.
// Performance impact is negligible (~7 lookups, few μs).
func (r *LookupRegistry) GetAllLookups() map[string]LookupDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Create a shallow copy to protect the registry's map structure
	// while allowing safe concurrent reads via RWMutex
	lookupsCopy := make(map[string]LookupDefinition, len(r.lookups))
	for id, lookup := range r.lookups {
		lookupsCopy[id] = lookup
	}

	return lookupsCopy
}

// GetLookupsForProbe returns all lookups relevant to a specific probe type
func (r *LookupRegistry) GetLookupsForProbe(probeType string) map[string]LookupDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]LookupDefinition)
	prefix := probeType + "."

	for id, lookup := range r.lookups {
		if len(id) > len(prefix) && id[:len(prefix)] == prefix {
			result[id] = lookup
		}
	}

	return result
}

// GetTextForValue returns the text representation of a numeric value
func (r *LookupRegistry) GetTextForValue(lookupID string, value int) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lookup, exists := r.lookups[lookupID]
	if !exists {
		return "", false
	}

	mapping, exists := lookup.Mappings[value]
	if !exists {
		return "", false
	}

	return mapping.Text, true
}

// GetSeverityForValue returns the severity level of a numeric value
func (r *LookupRegistry) GetSeverityForValue(lookupID string, value int) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lookup, exists := r.lookups[lookupID]
	if !exists {
		return "", false
	}

	mapping, exists := lookup.Mappings[value]
	if !exists {
		return "unknown", false
	}

	return mapping.Severity, true
}

// HasLookup checks if a lookup exists
func (r *LookupRegistry) HasLookup(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.lookups[id]
	return exists
}

// GetLookupCount returns the total number of lookups
func (r *LookupRegistry) GetLookupCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.lookups)
}

// IsHealthy checks if the registry was loaded successfully
func (r *LookupRegistry) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Registry is healthy if it has at least one lookup loaded
	return len(r.lookups) > 0
}
