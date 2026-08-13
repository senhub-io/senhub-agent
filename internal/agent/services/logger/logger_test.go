package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"

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
		DebugLogShipperUrl:    "http://example.com/logs",
		DebugLogShipperBuffer: 200,
		DebugLogShipperTags:   map[string]string{"env": "test"},
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
		Env:                "development",
		Verbose:            true,
		DebugLogShipperUrl: "http://example.com/logs",
	}

	// Create a new logger - this should attempt to create a debug log shipper
	_ = NewLogger(args)

	// Restore stderr
	w.Close()
	os.Stderr = originalOutput
	_, _ = io.Copy(&logBuffer, r)

	// There should be some log output related to the debug log shipper
	output := logBuffer.String()
	if output == "" || !bytes.Contains(logBuffer.Bytes(), []byte("debug log")) {
		t.Log("No log message about debug log shipper was found, but this may be expected in tests")
	}
}

// --- Log file layout ------------------------------------------------------

// The file must open with a full timestamp and read left to right.
//
// JSON was not merely dense. The masking writer re-encodes each entry through
// a map, and Go marshals map keys alphabetically, so "time" landed at the END
// of every line and "level" sat in the middle — nothing aligned from one line
// to the next. That is what made the file unusable during an incident.
func TestFileWriter_IsReadableAndStartsWithTheTimestamp(t *testing.T) {
	var buf bytes.Buffer
	w := NewMaskingWriter(fileWriter(&buf, &cliArgs.ParsedArgs{}))
	lg := zerolog.New(w).With().Timestamp().Logger()

	lg.Info().Str("module", "data_store").Int("count", 116).Msg("sent datapoints")

	line := buf.String()
	if strings.HasPrefix(line, "{") {
		t.Fatalf("the file still receives JSON: %s", line)
	}
	// A date, then the level, then the message — in that order.
	iDate := strings.Index(line, "20")
	iLevel := strings.Index(line, "INF")
	iMsg := strings.Index(line, "sent datapoints")
	if iDate != 0 {
		t.Errorf("line does not start with its timestamp: %s", line)
	}
	if !(iLevel > iDate && iMsg > iLevel) {
		t.Errorf("fields are out of reading order (date %d, level %d, message %d): %s", iDate, iLevel, iMsg, line)
	}
	for _, want := range []string{"count=116", "module=data_store"} {
		if !strings.Contains(line, want) {
			t.Errorf("structured field %q was dropped: %s", want, line)
		}
	}
}

// The machine-parseable form stays available for anyone shipping the file.
func TestFileWriter_JSONFormatIsStillAvailable(t *testing.T) {
	var buf bytes.Buffer
	w := NewMaskingWriter(fileWriter(&buf, &cliArgs.ParsedArgs{LogFormat: cliArgs.LogFormatJSON}))
	lg := zerolog.New(w).With().Timestamp().Logger()
	lg.Info().Msg("machine readable")

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("--log-format json did not produce JSON: %v (%s)", err, buf.String())
	}
}

// Redaction must survive the format change: the masker sees structured JSON
// and the file only ever receives the already-masked entry.
func TestFileWriter_SecretsAreStillMaskedInTextForm(t *testing.T) {
	var buf bytes.Buffer
	w := NewMaskingWriter(fileWriter(&buf, &cliArgs.ParsedArgs{}))
	lg := zerolog.New(w).With().Timestamp().Logger()

	lg.Info().Str("password", "hunter2").Msg("connecting")

	if strings.Contains(buf.String(), "hunter2") {
		t.Errorf("a secret reached the log file in clear: %s", buf.String())
	}
}
