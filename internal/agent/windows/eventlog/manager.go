//go:build windows

package eventlog

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Manager manages event collection from multiple channels
type Manager struct {
	checkpoints  *CheckpointStore
	api          API
	channels     []string
	maxEvents    int
	debug        bool
	includeXML   bool
	mutex        sync.RWMutex
}

// ManagerOption allows configuring the Manager
type ManagerOption func(*Manager)

// WithDebug enables debug mode
func WithDebug(debug bool) ManagerOption {
	return func(m *Manager) {
		m.debug = debug
	}
}

// WithMaxEvents sets the maximum events to collect per channel
func WithMaxEvents(maxEvents int) ManagerOption {
	return func(m *Manager) {
		if maxEvents > 0 {
			m.maxEvents = maxEvents
		}
	}
}

// WithIncludeXML includes raw XML in events
func WithIncludeXML(include bool) ManagerOption {
	return func(m *Manager) {
		m.includeXML = include
	}
}

// WithAPI explicitly sets the API to use
func WithAPI(api API) ManagerOption {
	return func(m *Manager) {
		m.api = api
	}
}

// WithCheckpointStore sets the checkpoint store
func WithCheckpointStore(store *CheckpointStore) ManagerOption {
	return func(m *Manager) {
		m.checkpoints = store
	}
}

// NewManager creates a new event log manager
func NewManager(channels []string, options ...ManagerOption) (*Manager, error) {
	if len(channels) == 0 {
		return nil, fmt.Errorf("at least one channel must be specified")
	}
	
	manager := &Manager{
		channels:    channels,
		maxEvents:   100, // Default max events
		includeXML:  false,
		debug:       false,
	}
	
	// Apply options
	for _, option := range options {
		option(manager)
	}
	
	// Create default checkpoint store if none provided
	if manager.checkpoints == nil {
		store, err := NewCheckpointStore("eventlog_checkpoints.json")
		if err != nil {
			return nil, fmt.Errorf("failed to create checkpoint store: %w", err)
		}
		manager.checkpoints = store
	}
	
	return manager, nil
}

// Init initializes the event log manager
func (m *Manager) Init() error {
	// Auto-detect API if not explicitly set
	if m.api == nil {
		api, err := GetPreferredAPI(m.includeXML)
		if err != nil {
			return fmt.Errorf("failed to get preferred API: %w", err)
		}
		if api == nil {
			return fmt.Errorf("no Windows Event Log API available")
		}
		m.api = api
	}
	
	return nil
}

// SetAPI changes the API used by the manager
func (m *Manager) SetAPI(api API) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	m.api = api
}

// GetCurrentAPI returns the name of the current API
func (m *Manager) GetCurrentAPI() string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	if m.api == nil {
		return "none"
	}
	
	return m.api.Name()
}

// ListChannels lists available channels
func (m *Manager) ListChannels(ctx context.Context) ([]string, error) {
	m.mutex.RLock()
	api := m.api
	m.mutex.RUnlock()
	
	if api == nil {
		return nil, fmt.Errorf("no API available")
	}
	
	return api.ListChannels(ctx)
}

// Start starts collecting events from all channels
func (m *Manager) Start(ctx context.Context) (<-chan *EventBatch, <-chan error) {
	eventChan := make(chan *EventBatch)
	errChan := make(chan error, 1)
	
	go func() {
		defer close(eventChan)
		defer close(errChan)
		
		var wg sync.WaitGroup
		
		// Create a separate context for cleanup
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		
		// Start a goroutine for each channel
		for _, channel := range m.channels {
			wg.Add(1)
			
			go func(channel string) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						errChan <- fmt.Errorf("panic in channel %s: %v", channel, r)
					}
				}()
				
				// Get API with read lock
				m.mutex.RLock()
				api := m.api
				m.mutex.RUnlock()
				
				if api == nil {
					errChan <- fmt.Errorf("no API available for channel %s", channel)
					return
				}
				
				// Get checkpoint
				cp := m.checkpoints.Get(channel)
				
				if m.debug {
					fmt.Printf("Starting collection for channel %s from position %d\n", channel, cp.Position)
				}
				
				// Subscribe to events
				batches, errs := api.SubscribeToEvents(ctx, channel, cp)
				
				// Handle events and errors
				for {
					select {
					case batch, ok := <-batches:
						if !ok {
							return
						}
						
						if len(batch.Events) > 0 {
							// Update checkpoint with last event position
							lastEvent := batch.Events[len(batch.Events)-1]
							m.checkpoints.Set(channel, batch.Position, lastEvent.TimeCreated, "")
							
							select {
							case eventChan <- batch:
								// Event batch sent
							case <-ctx.Done():
								return
							}
						}
						
					case err, ok := <-errs:
						if !ok {
							return
						}
						
						if err != nil {
							select {
							case errChan <- fmt.Errorf("error reading from channel %s: %w", channel, err):
								// Error sent
							case <-ctx.Done():
								return
							}
						}
						
					case <-ctx.Done():
						return
					}
				}
				
			}(channel)
		}
		
		// Wait for all goroutines to finish
		wg.Wait()
	}()
	
	return eventChan, errChan
}

// ReadEvents reads a batch of events from a channel
func (m *Manager) ReadEvents(ctx context.Context, channel string, maxEvents int) (*EventBatch, error) {
	if maxEvents <= 0 {
		maxEvents = m.maxEvents
	}
	
	m.mutex.RLock()
	api := m.api
	m.mutex.RUnlock()
	
	if api == nil {
		return nil, fmt.Errorf("no API available")
	}
	
	// Get checkpoint
	cp := m.checkpoints.Get(channel)
	
	// Open handle
	handle, err := api.Open(channel)
	if err != nil {
		return nil, fmt.Errorf("failed to open channel %s: %w", channel, err)
	}
	defer api.Close(handle)
	
	// Read events
	batch, err := api.Read(ctx, handle, maxEvents, cp)
	if err != nil {
		return nil, fmt.Errorf("failed to read events from channel %s: %w", channel, err)
	}
	
	// Update checkpoint if events were read
	if len(batch.Events) > 0 {
		lastEvent := batch.Events[len(batch.Events)-1]
		m.checkpoints.Set(channel, batch.Position, lastEvent.TimeCreated, "")
	}
	
	return batch, nil
}

// SaveCheckpoints saves the current checkpoints
func (m *Manager) SaveCheckpoints() error {
	return m.checkpoints.Save()
}

// Close closes the manager and saves checkpoints
func (m *Manager) Close() error {
	// Save checkpoints
	return m.checkpoints.Save()
}
