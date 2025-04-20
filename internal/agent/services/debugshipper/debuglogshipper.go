package debugshipper

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DebugLogShipper implements io.Writer interface for sending logs to a remote endpoint
// It buffers log entries and periodically flushes them to the configured destination
type DebugLogShipper struct {
	// Remote endpoint configuration
	endpoint   string
	httpClient *http.Client
	headers    map[string]string

	// Buffer management
	buffer     []string
	bufferSize int
	bufferLock sync.Mutex

	// Flush control
	flushInterval time.Duration
	flushTimer    *time.Timer
	flushChan     chan bool
	closed        bool
}

// Config holds configuration options for DebugLogShipper
type Config struct {
	// Required
	Endpoint string

	// Optional with defaults
	BufferSize    int
	FlushInterval time.Duration
	Headers       map[string]string
	Timeout       time.Duration
}

// DefaultConfig returns a Config with sensible default values
func DefaultConfig() *Config {
	return &Config{
		BufferSize:    100,
		FlushInterval: 10 * time.Second,
		Headers:       map[string]string{"Content-Type": "application/json"},
		Timeout:       30 * time.Second,
	}
}

// NewDebugLogShipper creates a new DebugLogShipper with the provided configuration
func NewDebugLogShipper(config *Config) (*DebugLogShipper, error) {
	if config.Endpoint == "" {
		return nil, ErrMissingEndpoint
	}

	// Apply defaults for missing values
	if config.BufferSize <= 0 {
		config.BufferSize = DefaultConfig().BufferSize
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = DefaultConfig().FlushInterval
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultConfig().Timeout
	}
	if config.Headers == nil {
		config.Headers = DefaultConfig().Headers
	}

	shipper := &DebugLogShipper{
		endpoint:      config.Endpoint,
		httpClient:    &http.Client{Timeout: config.Timeout},
		headers:       config.Headers,
		buffer:        make([]string, 0, config.BufferSize),
		bufferSize:    config.BufferSize,
		flushInterval: config.FlushInterval,
		flushChan:     make(chan bool, 1),
	}

	// Start flush timer
	shipper.flushTimer = time.AfterFunc(config.FlushInterval, func() {
		shipper.triggerFlush()
	})

	// Start worker goroutine for handling flush requests
	go shipper.flushWorker()

	return shipper, nil
}

// Write implements io.Writer interface
// It buffers the log entry and triggers a flush if buffer is full
func (s *DebugLogShipper) Write(p []byte) (n int, err error) {
	if s.closed {
		return 0, ErrShipperClosed
	}

	logEntry := string(p)

	s.bufferLock.Lock()
	s.buffer = append(s.buffer, logEntry)
	shouldFlush := len(s.buffer) >= s.bufferSize
	s.bufferLock.Unlock()

	if shouldFlush {
		s.triggerFlush()
	}

	return len(p), nil
}

// triggerFlush signals the flush worker to process the buffer
func (s *DebugLogShipper) triggerFlush() {
	select {
	case s.flushChan <- true:
		// Signal sent
	default:
		// Channel full, which means a flush is already pending
	}
}

// flushWorker handles buffer flushing in the background
func (s *DebugLogShipper) flushWorker() {
	for {
		select {
		case <-s.flushChan:
			s.flush()
			// Reset timer after flush
			if !s.closed {
				s.flushTimer.Reset(s.flushInterval)
			}
		}

		if s.closed {
			return
		}
	}
}

// flush sends buffered logs to the remote endpoint
func (s *DebugLogShipper) flush() {
	s.bufferLock.Lock()
	if len(s.buffer) == 0 {
		s.bufferLock.Unlock()
		return
	}

	// Take current buffer and reset it
	entries := s.buffer
	s.buffer = make([]string, 0, s.bufferSize)
	s.bufferLock.Unlock()

	// Create payload from buffer
	payload := strings.Join(entries, "\n")

	// Send logs asynchronously to avoid blocking
	go func(data string) {
		err := s.sendLogs(data)
		if err != nil {
			log.Printf("Error sending logs to remote endpoint: %v", err)
		}
	}(payload)
}

// sendLogs sends the payload to the remote endpoint
func (s *DebugLogShipper) sendLogs(payload string) error {
	req, err := http.NewRequest("POST", s.endpoint, strings.NewReader(payload))
	if err != nil {
		return err
	}

	// Add headers
	for key, value := range s.headers {
		req.Header.Set(key, value)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return ErrRemoteEndpointError
	}

	return nil
}

// Close implements io.Closer interface
// It flushes any remaining logs and cleans up resources
func (s *DebugLogShipper) Close() error {
	if s.closed {
		return nil
	}

	s.closed = true
	s.flushTimer.Stop()
	s.triggerFlush() // Final flush
	return nil
}