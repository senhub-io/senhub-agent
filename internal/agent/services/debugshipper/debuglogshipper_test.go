package debugshipper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewDebugLogShipper(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "Valid configuration",
			config: &Config{
				Endpoint:      "http://example.com/logs",
				BufferSize:    100,
				FlushInterval: 10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "Missing endpoint",
			config: &Config{
				BufferSize:    100,
				FlushInterval: 10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "Zero buffer size uses default",
			config: &Config{
				Endpoint:      "http://example.com/logs",
				BufferSize:    0,
				FlushInterval: 10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "Zero flush interval uses default",
			config: &Config{
				Endpoint:      "http://example.com/logs",
				BufferSize:    100,
				FlushInterval: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shipper, err := NewDebugLogShipper(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDebugLogShipper() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				// Verify default values are applied when needed
				if tt.config.BufferSize <= 0 && shipper.bufferSize != DefaultConfig().BufferSize {
					t.Errorf("Expected default buffer size %d, got %d", DefaultConfig().BufferSize, shipper.bufferSize)
				}
				if tt.config.FlushInterval <= 0 && shipper.flushInterval != DefaultConfig().FlushInterval {
					t.Errorf("Expected default flush interval %v, got %v", DefaultConfig().FlushInterval, shipper.flushInterval)
				}

				// Clean up
				shipper.Close()
			}
		})
	}
}

func TestDebugLogShipper_Write(t *testing.T) {
	// Create a test server to receive logs
	var receivedLogs []string
	var serverMutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverMutex.Lock()
		defer serverMutex.Unlock()

		// Read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		// Add logs to received list
		logs := strings.Split(string(body), "\n")
		receivedLogs = append(receivedLogs, logs...)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create shipper with small buffer and flush interval for testing
	config := &Config{
		Endpoint:      server.URL,
		BufferSize:    3, // Small buffer to test frequent flushes
		FlushInterval: 100 * time.Millisecond,
	}

	shipper, err := NewDebugLogShipper(config)
	if err != nil {
		t.Fatalf("Failed to create shipper: %v", err)
	}
	defer shipper.Close()

	// Write some logs (should trigger a flush after 3)
	testLogs := []string{
		`{"level":"debug","message":"test log 1"}`,
		`{"level":"info","message":"test log 2"}`,
		`{"level":"error","message":"test log 3"}`,
		`{"level":"debug","message":"test log 4"}`,
	}

	for _, log := range testLogs {
		n, err := shipper.Write([]byte(log))
		if err != nil {
			t.Errorf("Write() error = %v", err)
		}
		if n != len(log) {
			t.Errorf("Write() wrote %d bytes, expected %d", n, len(log))
		}
	}

	// Wait for flush to happen
	time.Sleep(200 * time.Millisecond)

	// Verify logs were received
	serverMutex.Lock()
	defer serverMutex.Unlock()

	if len(receivedLogs) < len(testLogs) {
		t.Errorf("Expected at least %d logs, got %d", len(testLogs), len(receivedLogs))
	}

	// Verify log content - only checking for required fields since timestamp and stream may be added
	for i, expected := range testLogs {
		if i >= len(receivedLogs) {
			break
		}
		// Instead of exact match, check that the received log contains the expected content
		if !strings.Contains(receivedLogs[i], `"level"`) || !strings.Contains(receivedLogs[i], `"message"`) {
			t.Errorf("Log %d missing required fields: got %s", i, receivedLogs[i])
		}

		// Extract expected level and message
		expectedLevel := strings.SplitN(expected, `"level":"`, 2)[1]
		expectedLevel = strings.SplitN(expectedLevel, `"`, 2)[0]

		expectedMsg := strings.SplitN(expected, `"message":"`, 2)[1]
		expectedMsg = strings.SplitN(expectedMsg, `"`, 2)[0]

		// Check if the received log contains the expected level and message
		if !strings.Contains(receivedLogs[i], `"level":"`+expectedLevel+`"`) {
			t.Errorf("Log %d level mismatch: expected to contain %s, got %s",
				i, expectedLevel, receivedLogs[i])
		}
		if !strings.Contains(receivedLogs[i], `"message":"`+expectedMsg+`"`) {
			t.Errorf("Log %d message mismatch: expected to contain %s, got %s",
				i, expectedMsg, receivedLogs[i])
		}
	}
}

func TestDebugLogShipper_Close(t *testing.T) {
	// Create a test server
	var received int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt64(&received, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create shipper with long flush interval
	config := &Config{
		Endpoint:      server.URL,
		BufferSize:    10,
		FlushInterval: 10 * time.Second, // Long interval
	}

	shipper, err := NewDebugLogShipper(config)
	if err != nil {
		t.Fatalf("Failed to create shipper: %v", err)
	}

	// Write a log
	_, err = shipper.Write([]byte(`{"level":"debug","message":"test log"}`))
	if err != nil {
		t.Errorf("Write() error = %v", err)
	}

	// Close should trigger a flush
	err = shipper.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Give some time for the flush to complete
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&received) == 0 {
		t.Error("Close() didn't trigger a flush")
	}

	// Write after close should fail
	_, err = shipper.Write([]byte(`{"level":"debug","message":"test log after close"}`))
	if err != ErrShipperClosed {
		t.Errorf("Write() after Close() error = %v, expected ErrShipperClosed", err)
	}
}
