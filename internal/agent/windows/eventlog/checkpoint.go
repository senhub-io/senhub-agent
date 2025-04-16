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
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Use atomic file write to prevent corruption
	tempFile := s.filePath + ".tmp"
	
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temporary checkpoint file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s.checkpoints); err != nil {
		return fmt.Errorf("failed to encode checkpoints: %w", err)
	}

	// Make sure data is on disk
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync checkpoint file: %w", err)
	}

	// Close the file before renaming
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close checkpoint file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, s.filePath); err != nil {
		return fmt.Errorf("failed to rename checkpoint file: %w", err)
	}

	s.lastFlush = time.Now()
	return nil
}

// load reads checkpoints from disk
func (s *CheckpointStore) load() error {
	file, err := os.Open(s.filePath)
	if err != nil {
		return fmt.Errorf("failed to open checkpoint file: %w", err)
	}
	defer file.Close()

	// Read all content
	content, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read checkpoint file: %w", err)
	}

	// Handle empty file
	if len(content) == 0 {
		s.checkpoints = make(map[string]*Checkpoint)
		return nil
	}

	// Decode JSON
	if err := json.Unmarshal(content, &s.checkpoints); err != nil {
		return fmt.Errorf("failed to decode checkpoints: %w", err)
	}

	return nil
}

// Close ensures all checkpoints are saved
func (s *CheckpointStore) Close() error {
	return s.Save()
}