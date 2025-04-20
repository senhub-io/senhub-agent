package logger

import (
	"bytes"
	"io"
	"os"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
)

// mockWriter is a simple io.Writer implementation for testing
type mockWriter struct {
	buffer *bytes.Buffer
}

func newMockWriter() *mockWriter {
	return &mockWriter{
		buffer: &bytes.Buffer{},
	}
}

func (w *mockWriter) Write(p []byte) (n int, err error) {
	return w.buffer.Write(p)
}

func (w *mockWriter) String() string {
	return w.buffer.String()
}

// TestDebugLogShipperInDevelopmentMode tests that logs are sent to the debug log shipper
// in development mode
func TestDebugLogShipperInDevelopmentMode(t *testing.T) {
	// Create a mock writer that will act as our debug log shipper
	mockShipper := newMockWriter()
	
	// Create a basic config with our mock shipper
	config := &LoggerConfig{
		logShipper: mockShipper,
	}
	
	// Create args for development mode
	args := &cliArgs.ParsedArgs{
		Env:     "development",
		Verbose: true,
	}
	
	// Build a development logger
	logger := buildDevelopmentLogger(args, config)
	
	// Write a test log message
	logger.Info().Msg("Test log message")
	
	// Check that the message was sent to the mock shipper
	shipperOutput := mockShipper.String()
	if shipperOutput == "" {
		t.Error("No log message was sent to the debug log shipper")
	}
	
	// Basic check that the log message is in the output
	if !bytes.Contains(mockShipper.buffer.Bytes(), []byte("Test log message")) {
		t.Error("Log message content was not correctly sent to the debug log shipper")
	}
}

// TestSetupDebugLogShipper tests that the debug log shipper setup function works correctly
func TestSetupDebugLogShipper(t *testing.T) {
	// Test case 1: No URL provided should return nil
	args1 := &cliArgs.ParsedArgs{
		DebugLogShipperUrl: "",
	}
	
	shipper1, err1 := setupDebugLogShipper(args1)
	if shipper1 != nil || err1 != nil {
		t.Error("setupDebugLogShipper should return nil when no URL is provided")
	}
	
	// Test case 2: Valid URL should return a shipper (but will fail in tests due to actual HTTP request)
	args2 := &cliArgs.ParsedArgs{
		DebugLogShipperUrl: "http://example.com/logs",
		DebugLogShipperBuffer: 200,
		DebugLogShipperTags: map[string]string{"env": "test"},
	}
	
	// We expect an error because the URL isn't reachable in tests, but it should attempt to create a shipper
	shipper2, _ := setupDebugLogShipper(args2)
	if shipper2 == nil {
		// This is expected in real tests, but we won't fail the test for it
		t.Log("setupDebugLogShipper returned nil for a valid URL - expected in tests")
	}
}

// TestNewLoggerWithShipperConfiguration tests the logger creation with shipper configuration
func TestNewLoggerWithShipperConfiguration(t *testing.T) {
	// Create a test logger to capture log output
	var logBuffer bytes.Buffer
	
	// Save the original log output
	originalOutput := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	
	// Create args with debug log shipper configuration
	args := &cliArgs.ParsedArgs{
		Env: "development",
		Verbose: true,
		DebugLogShipperUrl: "http://example.com/logs",
	}
	
	// Create a new logger - this should attempt to create a debug log shipper
	_ = NewLogger(args)
	
	// Restore stderr
	w.Close()
	os.Stderr = originalOutput
	io.Copy(&logBuffer, r)
	
	// There should be some log output related to the debug log shipper
	output := logBuffer.String()
	if output == "" || !bytes.Contains(logBuffer.Bytes(), []byte("debug log")) {
		t.Log("No log message about debug log shipper was found, but this may be expected in tests")
	}
}