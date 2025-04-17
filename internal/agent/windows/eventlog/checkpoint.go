//go:build windows

package eventlog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CheckpointStore manages the persistence of checkpoints
type CheckpointStore struct {
	filePath     string
	checkpoints  map[string]*Checkpoint
	mutex        sync.RWMutex
	flushTimeout time.Duration
	lastFlush    time.Time
}

// NewCheckpointStore creates a new checkpoint store
func NewCheckpointStore(filePath string) (*CheckpointStore, error) {
	if filePath == "" {
		return nil, fmt.Errorf("checkpoint file path cannot be empty")
	}

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	store := &CheckpointStore{
		filePath:     filePath,
		checkpoints:  make(map[string]*Checkpoint),
		flushTimeout: 30 * time.Second,
		lastFlush:    time.Now(),
	}

	// Load existing checkpoints if the file exists
	if _, err := os.Stat(filePath); err == nil {
		if err := store.load(); err != nil {
			return nil, fmt.Errorf("failed to load checkpoints: %w", err)
		}
	}

	return store, nil
}

// Get retrieves a checkpoint for a channel
func (s *CheckpointStore) Get(channel string) *Checkpoint {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if cp, exists := s.checkpoints[channel]; exists {
		return cp
	}

	// Create a new checkpoint if none exists
	return &Checkpoint{
		Channel:      channel,
		Position:     0,
		Timestamp:    time.Time{},
		LastModified: time.Now(),
	}
}

// Set updates a checkpoint for a channel
func (s *CheckpointStore) Set(channel string, position uint64, timestamp time.Time, bookmarkXML string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	cp, exists := s.checkpoints[channel]
	if !exists {
		cp = &Checkpoint{
			Channel: channel,
		}
		s.checkpoints[channel] = cp
	}

	cp.Position = position
	cp.Timestamp = timestamp
	cp.LastModified = time.Now()
	
	if bookmarkXML != "" {
		cp.BookmarkXML = bookmarkXML
	}

	// Auto-save if enough time has passed since last flush
	if time.Since(s.lastFlush) > s.flushTimeout {
		// Ignoring error here - we'll try again later
		_ = s.Save()
	}
}

// Save persists the checkpoints to disk
func (s *CheckpointStore) Save() error {
	// Use a recovery function to prevent panic
	var saveErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				saveErr = fmt.Errorf("panic during checkpoint save: %v", r)
			}
		}()
		
		// We need to use a write lock here to prevent concurrent saves
		s.mutex.Lock()
		defer s.mutex.Unlock()
		
		// Check if there are any checkpoints to save
		if len(s.checkpoints) == 0 {
			s.lastFlush = time.Now()
			return
		}

		// Use atomic file write to prevent corruption
		tempFile := s.filePath + ".tmp"
		
		// Ensure the directory exists
		dir := filepath.Dir(s.filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			saveErr = fmt.Errorf("failed to create checkpoint directory: %w", err)
			return
		}
		
		// Create the temporary file with safe permissions
		file, err := os.OpenFile(tempFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			saveErr = fmt.Errorf("failed to create temporary checkpoint file: %w", err)
			return
		}
		
		// Cleanup function to ensure we always close the file and remove it if there's an error
		closeFile := func() {
			file.Close()
			// Try to remove the temporary file if it exists and we had an error
			if saveErr != nil {
				os.Remove(tempFile) // Ignoring error here, we're already handling another error
			}
		}
		defer closeFile()

		// Make a deep copy of checkpoints to avoid marshalling issues
		checkpointsCopy := make(map[string]*Checkpoint, len(s.checkpoints))
		for k, v := range s.checkpoints {
			if v == nil {
				continue
			}
			cp := *v // Make a copy of the checkpoint
			checkpointsCopy[k] = &cp
		}

		// Marshal to JSON with pretty formatting
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(checkpointsCopy); err != nil {
			saveErr = fmt.Errorf("failed to encode checkpoints: %w", err)
			return
		}

		// Make sure data is on disk
		if err := file.Sync(); err != nil {
			saveErr = fmt.Errorf("failed to sync checkpoint file: %w", err)
			return
		}

		// Close the file before renaming
		if err := file.Close(); err != nil {
			saveErr = fmt.Errorf("failed to close checkpoint file: %w", err)
			return
		}

		// Atomic rename with retry
		for attempt := 0; attempt < 3; attempt++ {
			err := os.Rename(tempFile, s.filePath)
			if err == nil {
				break
			}
			
			if attempt == 2 {
				saveErr = fmt.Errorf("failed to rename checkpoint file after retries: %w", err)
				return
			}
			
			// If rename failed and this isn't the last retry, wait a moment before retrying
			time.Sleep(100 * time.Millisecond)
		}

		s.lastFlush = time.Now()
	}()
	
	return saveErr
}

// load reads checkpoints from disk
func (s *CheckpointStore) load() error {
	// Use a recovery function to prevent panic
	var loadErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				loadErr = fmt.Errorf("panic during checkpoint load: %v", r)
			}
		}()
		
		// Initialize empty checkpoints map in case of failure
		s.checkpoints = make(map[string]*Checkpoint)
		
		// Check if file exists
		if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
			// File doesn't exist, not an error, just use empty checkpoints
			return
		}
		
		// Open the file with retry
		var file *os.File
		var err error
		
		for attempt := 0; attempt < 3; attempt++ {
			file, err = os.Open(s.filePath)
			if err == nil {
				break
			}
			
			if attempt == 2 {
				loadErr = fmt.Errorf("failed to open checkpoint file after retries: %w", err)
				return
			}
			
			// If open failed and this isn't the last retry, wait a moment before retrying
			time.Sleep(100 * time.Millisecond)
		}
		
		if file == nil {
			// Failed to open file but didn't set error
			loadErr = fmt.Errorf("failed to open checkpoint file")
			return
		}
		
		defer file.Close()

		// Read all content with timeout protection
		var content []byte
		readDone := make(chan struct{})
		
		go func() {
			defer close(readDone)
			content, err = io.ReadAll(file)
		}()
		
		// Wait for read to complete or timeout
		select {
		case <-readDone:
			// Read completed
			if err != nil {
				loadErr = fmt.Errorf("failed to read checkpoint file: %w", err)
				return
			}
		case <-time.After(5 * time.Second):
			loadErr = fmt.Errorf("timeout reading checkpoint file")
			return
		}

		// Handle empty file
		if len(content) == 0 {
			return
		}

		// Define a temporary map to decode into
		tempCheckpoints := make(map[string]*Checkpoint)
		
		// Decode JSON
		if err := json.Unmarshal(content, &tempCheckpoints); err != nil {
			// Try to read a backup file if the main file is corrupted
			loadErr = fmt.Errorf("failed to decode checkpoints (will use empty state): %w", err)
			return
		}
		
		// Validate checkpoint data
		for channel, cp := range tempCheckpoints {
			if cp == nil {
				// Skip nil checkpoints
				continue
			}
			
			// Validate channel name matches map key
			if cp.Channel == "" {
				cp.Channel = channel
			}
			
			// Always use current time as LastModified if missing
			if cp.LastModified.IsZero() {
				cp.LastModified = time.Now()
			}
			
			// Add to our checkpoints map
			s.checkpoints[channel] = cp
		}
	}()
	
	return loadErr
}

// Close ensures all checkpoints are saved
func (s *CheckpointStore) Close() error {
	return s.Save()
}