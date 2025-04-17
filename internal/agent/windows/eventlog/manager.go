//go:build windows

package eventlog

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
	
	"golang.org/x/sys/windows"
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
	// Wrap the entire function in a recover to prevent crashes
	var result *EventBatch
	var resultErr error
	
	// Create a protected function to recover from any panics
	func() {
		defer func() {
			if r := recover(); r != nil {
				resultErr = fmt.Errorf("recovered from panic in ReadEvents: %v", r)
			}
		}()
		
		if maxEvents <= 0 {
			maxEvents = m.maxEvents
		}
		
		m.mutex.RLock()
		api := m.api
		m.mutex.RUnlock()
		
		if api == nil {
			resultErr = fmt.Errorf("no API available")
			return
		}
		
		// Get checkpoint
		cp := m.checkpoints.Get(channel)
		
		// Open handle with retry
		var handle windows.Handle
		var err error
		
		// Try opening the channel with a few retries
		for attempts := 0; attempts < 3; attempts++ {
			handle, err = api.Open(channel)
			if err == nil && handle != 0 {
				break
			}
			
			// If we got an error and this isn't the last attempt, wait a moment and retry
			if attempts < 2 {
				// Small delay before retry
				select {
				case <-ctx.Done():
					resultErr = ctx.Err()
					return
				case <-time.After(100 * time.Millisecond):
					// Continue to retry
				}
			}
		}
		
		if err != nil {
			resultErr = fmt.Errorf("failed to open channel %s after retries: %w", channel, err)
			return
		}
		
		if handle == 0 {
			resultErr = fmt.Errorf("failed to get valid handle for channel %s", channel)
			return
		}
		
		// Ensure handle is closed no matter what
		defer func() {
			closeErr := api.Close(handle)
			if closeErr != nil && resultErr == nil {
				resultErr = fmt.Errorf("error closing handle for channel %s: %w", channel, closeErr)
			}
		}()
		
		// Read events with context
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		
		// Use a separate function to read events that can recover from internal panics
		func() {
			defer func() {
				if r := recover(); r != nil {
					resultErr = fmt.Errorf("recovered from panic while reading events: %v", r)
				}
			}()
			
			batch, err := api.Read(readCtx, handle, maxEvents, cp)
			if err != nil {
				resultErr = fmt.Errorf("failed to read events from channel %s: %w", channel, err)
				return
			}
			
			// Check if batch is valid
			if batch == nil {
				resultErr = fmt.Errorf("nil batch returned from Read operation")
				return
			}
			
			// Update checkpoint if events were read
			if len(batch.Events) > 0 {
				// Validate there's at least one valid event
				if len(batch.Events) > 0 && !batch.Events[len(batch.Events)-1].TimeCreated.IsZero() {
					lastEvent := batch.Events[len(batch.Events)-1]
					m.checkpoints.Set(channel, batch.Position, lastEvent.TimeCreated, "")
				}
			}
			
			result = batch
		}()
	}()
	
	// Return result or error
	if resultErr != nil {
		// On error, return an empty batch to avoid nil pointer
		return &EventBatch{
			Channel: channel,
			Events:  []Event{},
		}, resultErr
	}
	
	// If something went wrong but we didn't get an error
	if result == nil {
		return &EventBatch{
			Channel: channel,
			Events:  []Event{},
		}, fmt.Errorf("unknown error occurred, no batch was returned")
	}
	
	return result, nil
}

// SaveCheckpoints saves the current checkpoints
func (m *Manager) SaveCheckpoints() error {
	return m.checkpoints.Save()
}

// Close closes the manager and saves checkpoints
func (m *Manager) Close() error {
	// Wrap in a recovery function to ensure we don't panic during shutdown
	var err error
	
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic recovered during Close: %v", r)
			}
		}()
		
		// Save checkpoints with timeout protection
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		done := make(chan struct{})
		var saveErr error
		
		go func() {
			defer close(done)
			saveErr = m.checkpoints.Save()
		}()
		
		// Wait for save to complete or timeout
		select {
		case <-done:
			// Save completed
			if saveErr != nil {
				err = saveErr
			}
		case <-saveCtx.Done():
			err = fmt.Errorf("timeout saving checkpoints: %v", saveCtx.Err())
		}
		
		// Force garbage collection to clean up any remaining resources
		runtime.GC()
	}()
	
	return err
}
