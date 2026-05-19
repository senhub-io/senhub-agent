package configuration

import (
	"context"
	"encoding/json"
	"sync"
)

// MockConfiguration is a lightweight in-memory ConfigurationProvider
// suitable for tests. Pre-0.2.0 the configuration package shipped a
// `NewMockRemoteConfiguration` helper backed by the real
// RemoteConfiguration struct; that was deleted alongside online mode.
// This replacement satisfies the same interfaces (ConfigurationProvider,
// auto_update's ConfigSource) without dragging in the SaaS-loader
// machinery.
type MockConfiguration struct {
	mu        sync.Mutex
	data      RemoteConfigurationData
	callbacks []func(string)
}

// NewMockConfiguration parses the provided JSON snippet (or `{}` when
// empty) into a RemoteConfigurationData and returns a mock provider.
// Mirrors the constructor signature pre-0.2.0 code used so tests can
// migrate by swapping the function name.
func NewMockConfiguration(_ /*url*/, config string) *MockConfiguration {
	if config == "" {
		config = `{"storage":[],"probes":[],"agent":{}}`
	}
	var parsed RemoteConfigurationData
	_ = json.Unmarshal([]byte(config), &parsed)
	return &MockConfiguration{data: parsed}
}

// NewMockRemoteConfiguration is a name-compatibility shim kept so tests
// that imported the pre-0.2.0 helper keep compiling. New tests should
// call NewMockConfiguration directly.
func NewMockRemoteConfiguration(url string, config string) *MockConfiguration {
	return NewMockConfiguration(url, config)
}

// GetName satisfies ConfigurationProvider.
func (m *MockConfiguration) GetName() string { return "MockConfiguration" }

// GetConfiguration returns the in-memory snapshot.
func (m *MockConfiguration) GetConfiguration() RemoteConfigurationData {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data
}

// OnConfigChanged registers a callback fired by NotifyChange. Tests
// can drive the callback directly to exercise reload behaviour.
func (m *MockConfiguration) OnConfigChanged(callback func(string)) {
	m.mu.Lock()
	m.callbacks = append(m.callbacks, callback)
	m.mu.Unlock()
}

// NotifyChange fires every registered callback with the given source
// label. Useful for tests that need to verify reload behaviour.
func (m *MockConfiguration) NotifyChange(source string) {
	m.mu.Lock()
	cbs := append([]func(string){}, m.callbacks...)
	m.mu.Unlock()
	for _, cb := range cbs {
		cb(source)
	}
}

// SetConfiguration overwrites the snapshot. Lets tests stage state
// without going through the constructor twice.
func (m *MockConfiguration) SetConfiguration(data RemoteConfigurationData) {
	m.mu.Lock()
	m.data = data
	m.mu.Unlock()
}

// Start is a no-op so MockConfiguration satisfies ConfigurationProvider.
func (m *MockConfiguration) Start(_ chan struct{}) error { return nil }

// Shutdown is a no-op so MockConfiguration satisfies ConfigurationProvider.
func (m *MockConfiguration) Shutdown(_ context.Context) error { return nil }

// GetCacheConfig returns the cache block, falling back to a sensible
// default when the mock data didn't include one.
func (m *MockConfiguration) GetCacheConfig() *CacheConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data.Cache != nil {
		return m.data.Cache
	}
	return &CacheConfig{RetentionMinutes: 5}
}
