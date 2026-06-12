package logger

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

func TestSetModuleLogLevel(t *testing.T) {
	// Reset module levels for test
	originalLevels := GetModuleLogLevels()
	defer func() {
		mutateLevelState(func(st *levelState) {
			st.levels = originalLevels
		})
	}()

	// Test setting a log level
	SetModuleLogLevel("test.module", zerolog.ErrorLevel)

	if got := GetModuleLogLevel("test.module"); got != zerolog.ErrorLevel {
		t.Errorf("Expected test.module level to be ErrorLevel, got %v", got)
	}
}

func TestSetModuleLogLevels(t *testing.T) {
	// Reset module levels for test
	originalLevels := GetModuleLogLevels()
	defer func() {
		mutateLevelState(func(st *levelState) {
			st.levels = originalLevels
		})
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

	if got := GetModuleLogLevel("test.module1"); got != zerolog.DebugLevel {
		t.Errorf("Expected test.module1 level to be DebugLevel, got %v", got)
	}
	if got := GetModuleLogLevel("test.module2"); got != zerolog.WarnLevel {
		t.Errorf("Expected test.module2 level to be WarnLevel, got %v", got)
	}
	if got := GetModuleLogLevel("test.module3"); got != zerolog.Disabled {
		t.Errorf("Expected test.module3 level to be Disabled, got %v", got)
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
	originalLevels := GetModuleLogLevels()
	defer func() {
		mutateLevelState(func(st *levelState) {
			st.levels = originalLevels
		})
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

	// Ensure we got a copy, not the live state
	levels["test.module1"] = zerolog.InfoLevel
	if GetModuleLogLevel("test.module1") != zerolog.DebugLevel {
		t.Error("Modifying returned map should not affect original")
	}
}

func TestIsModuleEnabled_ExactMatch(t *testing.T) {
	st := &levelState{debugModules: map[string]bool{"probe.veeam": true}}

	if !isModuleEnabled(st, "probe.veeam") {
		t.Error("exact match should be enabled")
	}
	if isModuleEnabled(st, "probe.citrix") {
		t.Error("non-matching module should not be enabled")
	}
}

func TestIsModuleEnabled_PrefixMatch(t *testing.T) {
	st := &levelState{debugModules: map[string]bool{"probe": true}}

	if !isModuleEnabled(st, "probe.veeam") {
		t.Error("prefix 'probe' should match 'probe.veeam'")
	}
	if !isModuleEnabled(st, "probe.citrix.client") {
		t.Error("prefix 'probe' should match 'probe.citrix.client'")
	}
	if isModuleEnabled(st, "strategy.http") {
		t.Error("prefix 'probe' should not match 'strategy.http'")
	}
	if isModuleEnabled(st, "probeX") {
		t.Error("prefix 'probe' should not match 'probeX' (no dot separator)")
	}
}

func TestSelectiveDebugMode_ReadsGlobalState(t *testing.T) {
	orig := levelStatePo.Load()
	defer levelStatePo.Store(orig)

	var buf bytes.Buffer
	baseLogger := zerolog.New(&buf).Level(zerolog.DebugLevel)

	// Create logger BEFORE enabling selective mode
	moduleLogger := NewModuleLogger(&baseLogger, "probe.veeam")

	// Enable selective mode AFTER creation — should still take effect (no staling)
	mutateLevelState(func(st *levelState) {
		st.selective = true
		st.debugModules = map[string]bool{"probe.citrix": true}
	})

	moduleLogger.Debug().Msg("should be filtered")
	if buf.Len() > 0 {
		t.Error("debug should be filtered: probe.veeam not in debugModules")
	}

	// Now enable probe.veeam
	mutateLevelState(func(st *levelState) {
		st.debugModules["probe.veeam"] = true
	})
	buf.Reset()
	moduleLogger.Debug().Msg("should appear")
	if buf.Len() == 0 {
		t.Error("debug should appear after enabling probe.veeam in global state")
	}
}

// TestLevelState_RaceTogglingUnderLoad is the #274 acceptance: hammer
// the Debug read path while the runtime log-level endpoint's write
// path toggles levels — the old package-level maps panicked here under
// the race detector.
func TestLevelState_RaceTogglingUnderLoad(t *testing.T) {
	orig := levelStatePo.Load()
	defer levelStatePo.Store(orig)

	baseLogger := zerolog.New(io.Discard).Level(zerolog.DebugLevel)
	ml := NewModuleLogger(&baseLogger, "probe.veeam")

	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					ml.Debug().Msg("load")
					_ = GetModuleLogLevel("probe.veeam")
					_ = GetModuleLogLevels()
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		SetModuleLogLevel("probe.veeam", zerolog.DebugLevel)
		_ = SetModuleLogLevels([]ModuleLogConfig{{Module: "probe.veeam", Level: "error"}})
	}
	close(done)
	wg.Wait()
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
