package logger

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
)

func TestSetModuleLogLevel(t *testing.T) {
	// Reset module levels for test
	originalLevels := make(map[string]zerolog.Level)
	for k, v := range moduleLogLevels {
		originalLevels[k] = v
	}
	defer func() {
		// Restore original levels
		for k, v := range originalLevels {
			moduleLogLevels[k] = v
		}
	}()

	// Test setting a log level
	SetModuleLogLevel("test.module", zerolog.ErrorLevel)

	if moduleLogLevels["test.module"] != zerolog.ErrorLevel {
		t.Errorf("Expected test.module level to be ErrorLevel, got %v", moduleLogLevels["test.module"])
	}
}

func TestSetModuleLogLevels(t *testing.T) {
	// Reset module levels for test
	originalLevels := make(map[string]zerolog.Level)
	for k, v := range moduleLogLevels {
		originalLevels[k] = v
	}
	defer func() {
		// Restore original levels
		for k, v := range originalLevels {
			moduleLogLevels[k] = v
		}
	}()

	configs := []ModuleLogConfig{
		{Module: "test.module1", Level: "debug"},
		{Module: "test.module2", Level: "warn"},
		{Module: "test.module3", Level: "disabled"},
	}

	err := SetModuleLogLevels(configs)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if moduleLogLevels["test.module1"] != zerolog.DebugLevel {
		t.Errorf("Expected test.module1 level to be DebugLevel, got %v", moduleLogLevels["test.module1"])
	}
	if moduleLogLevels["test.module2"] != zerolog.WarnLevel {
		t.Errorf("Expected test.module2 level to be WarnLevel, got %v", moduleLogLevels["test.module2"])
	}
	if moduleLogLevels["test.module3"] != zerolog.Disabled {
		t.Errorf("Expected test.module3 level to be Disabled, got %v", moduleLogLevels["test.module3"])
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected zerolog.Level
	}{
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"fatal", zerolog.FatalLevel},
		{"panic", zerolog.PanicLevel},
		{"disabled", zerolog.Disabled},
		{"invalid", zerolog.InfoLevel}, // Default fallback
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result, err := parseLogLevel(test.input)
			if err != nil {
				t.Errorf("Expected no error for %s, got %v", test.input, err)
			}
			if result != test.expected {
				t.Errorf("Expected %v for %s, got %v", test.expected, test.input, result)
			}
		})
	}
}

func TestNewModuleLogger(t *testing.T) {
	// Create a base logger
	var buf bytes.Buffer
	baseLogger := zerolog.New(&buf).With().Timestamp().Logger()

	// Set specific level for test module
	SetModuleLogLevel("test.module", zerolog.WarnLevel)

	// Create module logger
	moduleLogger := NewModuleLogger(&baseLogger, "test.module")

	// Test that debug message is filtered out (level is WARN)
	moduleLogger.Debug().Msg("This should not appear")
	if buf.Len() > 0 {
		t.Error("Debug message should have been filtered out")
	}

	// Test that warn message appears
	buf.Reset()
	moduleLogger.Warn().Msg("This should appear")
	if buf.Len() == 0 {
		t.Error("Warn message should have appeared")
	}

	// Check that the module field is present in the log
	logOutput := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("test.module")) {
		t.Errorf("Expected module name in log output, got: %s", logOutput)
	}
}

func TestGetModuleLogLevels(t *testing.T) {
	// Reset module levels for test
	originalLevels := make(map[string]zerolog.Level)
	for k, v := range moduleLogLevels {
		originalLevels[k] = v
	}
	defer func() {
		// Restore original levels
		for k, v := range originalLevels {
			moduleLogLevels[k] = v
		}
	}()

	// Set some test levels
	SetModuleLogLevel("test.module1", zerolog.DebugLevel)
	SetModuleLogLevel("test.module2", zerolog.ErrorLevel)

	levels := GetModuleLogLevels()

	if levels["test.module1"] != zerolog.DebugLevel {
		t.Errorf("Expected test.module1 to be DebugLevel, got %v", levels["test.module1"])
	}
	if levels["test.module2"] != zerolog.ErrorLevel {
		t.Errorf("Expected test.module2 to be ErrorLevel, got %v", levels["test.module2"])
	}

	// Ensure we got a copy, not the original map
	levels["test.module1"] = zerolog.InfoLevel
	if moduleLogLevels["test.module1"] != zerolog.DebugLevel {
		t.Error("Modifying returned map should not affect original")
	}
}

func TestModuleLoggerWithUnknownModule(t *testing.T) {
	// Create a base logger
	var buf bytes.Buffer
	baseLogger := zerolog.New(&buf).With().Timestamp().Logger()

	// Create module logger for unknown module (should use InfoLevel default)
	moduleLogger := NewModuleLogger(&baseLogger, "unknown.module")

	// Test that debug message is filtered out (default level is INFO)
	moduleLogger.Debug().Msg("This should not appear")
	if buf.Len() > 0 {
		t.Error("Debug message should have been filtered out for unknown module")
	}

	// Test that info message appears
	buf.Reset()
	moduleLogger.Info().Msg("This should appear")
	if buf.Len() == 0 {
		t.Error("Info message should have appeared for unknown module")
	}
}
