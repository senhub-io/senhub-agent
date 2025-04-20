package debugshipper

import (
	"encoding/json"
	"fmt"
	"io"
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
	config     *Config

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
	
	// VictoriaLogs specific options
	StreamField string   // Field to use as stream identifier (_stream_fields)
	TimeField   string   // Field to use as timestamp (_time_field)
	MsgField    string   // Field to use as message (_msg_field)
	Tags        map[string]string // Additional tags to add to every log entry
}

// DefaultConfig returns a Config with sensible default values
func DefaultConfig() *Config {
	return &Config{
		BufferSize:    100,
		FlushInterval: 10 * time.Second,
		Headers:       map[string]string{"Content-Type": "application/stream+json"},
		Timeout:       30 * time.Second,
		StreamField:   "stream",
		TimeField:     "timestamp",
		MsgField:      "message",
		Tags:          map[string]string{},
	}
}

// NewDebugLogShipper creates a new DebugLogShipper with the provided configuration
func NewDebugLogShipper(config *Config) (*DebugLogShipper, error) {
	if config.Endpoint == "" {
		return nil, ErrMissingEndpoint
	}

	// Apply defaults for missing values
	defaultConfig := DefaultConfig()
	if config.BufferSize <= 0 {
		config.BufferSize = defaultConfig.BufferSize
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = defaultConfig.FlushInterval
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultConfig.Timeout
	}
	if config.Headers == nil {
		config.Headers = defaultConfig.Headers
	}
	if config.StreamField == "" {
		config.StreamField = defaultConfig.StreamField
	}
	if config.TimeField == "" {
		config.TimeField = defaultConfig.TimeField
	}
	if config.MsgField == "" {
		config.MsgField = defaultConfig.MsgField
	}

	shipper := &DebugLogShipper{
		endpoint:      config.Endpoint,
		httpClient:    &http.Client{Timeout: config.Timeout},
		headers:       config.Headers,
		config:        config,
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

	// Try to parse the JSON log entry
	var logEntry map[string]interface{}
	if err := json.Unmarshal(p, &logEntry); err != nil {
		// If not valid JSON, create a simple log entry with the raw message
		logEntry = map[string]interface{}{
			s.config.MsgField: string(p),
			"timestamp": time.Now().UnixNano() / int64(time.Millisecond), // Use current time in milliseconds
			"stream": "agent",
		}
	}
	
	// Add any configured tags
	for k, v := range s.config.Tags {
		logEntry[k] = v
	}
	
	// Ensure stream field exists
	if _, ok := logEntry[s.config.StreamField]; !ok {
		logEntry[s.config.StreamField] = "agent"
	}
	
	// Ensure timestamp field exists (use 0 to let VictoriaLogs use server time)
	if _, ok := logEntry[s.config.TimeField]; !ok {
		logEntry[s.config.TimeField] = "0"
	}
	
	// Convert back to JSON string
	jsonBytes, err := json.Marshal(logEntry)
	if err != nil {
		return 0, err
	}
	
	jsonStr := string(jsonBytes)

	s.bufferLock.Lock()
	s.buffer = append(s.buffer, jsonStr)
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
	// Construct the VictoriaLogs JSON Stream API endpoint URL with query parameters
	url := s.endpoint
	
	// If the endpoint doesn't end with "/jsonline", append it assuming it's VictoriaLogs
	if !strings.HasSuffix(url, "/jsonline") && !strings.Contains(url, "/jsonline?") {
		if strings.HasSuffix(url, "/") {
			url += "insert/jsonline"
		} else {
			url += "/insert/jsonline"
		}
	}
	
	// Add VictoriaLogs specific query parameters if not already present
	if strings.HasSuffix(url, "/jsonline") || strings.Contains(url, "/jsonline?") {
		// Parse the URL to manipulate query parameters
		parsedURL, err := http.NewRequest("POST", url, nil)
		if err != nil {
			return err
		}
		
		// Get the current query values
		q := parsedURL.URL.Query()
		
		// Only add parameters if they're not already set
		if q.Get("_stream_fields") == "" && s.config.StreamField != "" {
			q.Add("_stream_fields", s.config.StreamField)
		}
		if q.Get("_time_field") == "" && s.config.TimeField != "" {
			q.Add("_time_field", s.config.TimeField)
		}
		if q.Get("_msg_field") == "" && s.config.MsgField != "" {
			q.Add("_msg_field", s.config.MsgField)
		}
		
		// Update the URL with new query parameters
		parsedURL.URL.RawQuery = q.Encode()
		url = parsedURL.URL.String()
	}
	
	// Create the request
	req, err := http.NewRequest("POST", url, strings.NewReader(payload))
	if err != nil {
		return err
	}

	// Add headers
	for key, value := range s.headers {
		req.Header.Set(key, value)
	}

	// Send the request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		// Read the response body for error details
		errorBody, _ := io.ReadAll(resp.Body)
		if len(errorBody) > 0 {
			return fmt.Errorf("%w: %s (Status: %d)", ErrRemoteEndpointError, string(errorBody), resp.StatusCode)
		}
		return fmt.Errorf("%w (Status: %d)", ErrRemoteEndpointError, resp.StatusCode)
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