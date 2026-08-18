package logger

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/cliArgs"
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

// A module with no explicit override follows the surrounding levels rather
// than being silenced for not appearing in a map.
//
// This test used to assert the opposite — that an unknown module has its debug
// suppressed — which is exactly the defect: the override map held sixteen
// names, the code creates module loggers under more than a hundred, and every
// module written after that map was frozen stayed mute under --verbose. The
// contract is now that quietness comes from the logger's level, and the map
// only overrides it.
func TestModuleLoggerWithUnknownModule(t *testing.T) {
	withRestoredLogState(t)
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	mutateLevelState(func(st *levelState) {
		st.selective = false
		st.levels = map[string]zerolog.Level{}
	})

	var buf bytes.Buffer
	// Quiet base logger: no debug expected, and the reason is the level.
	quiet := zerolog.New(&buf).Level(zerolog.InfoLevel).With().Timestamp().Logger()
	NewModuleLogger(&quiet, "unknown.module").Debug().Msg("This should not appear")
	if buf.Len() > 0 {
		t.Errorf("debug appeared through a logger set to Info: %q", buf.String())
	}

	// Verbose base logger: the same unknown module must now speak.
	buf.Reset()
	baseLogger := zerolog.New(&buf).Level(zerolog.DebugLevel).With().Timestamp().Logger()
	moduleLogger := NewModuleLogger(&baseLogger, "unknown.module")
	moduleLogger.Debug().Msg("This should appear under verbose")
	if buf.Len() == 0 {
		t.Error("an unknown module stayed silent under a debug-level logger")
	}

	// Test that info message appears
	buf.Reset()
	moduleLogger.Info().Msg("This should appear")
	if buf.Len() == 0 {
		t.Error("Info message should have appeared for unknown module")
	}
}

// --- Debug mode, end to end through NewLogger -----------------------------
//
// These drive NewLogger rather than re-creating what it does. The previous
// tests for selective mode set the level state by hand and passed, while the
// feature had never emitted a single line in production — because the part
// they did not copy was the one that broke it: NewLogger pinned zerolog's
// global level to Info, and the global level is a hard floor that drops an
// event before any per-module logic runs.

func withRestoredLogState(t *testing.T) {
	t.Helper()
	orig := levelStatePo.Load()
	origGlobal := zerolog.GlobalLevel()
	t.Cleanup(func() {
		levelStatePo.Store(orig)
		zerolog.SetGlobalLevel(origGlobal)
	})
}

// --verbose --filter <module> must leave the floor low enough for the module
// gate to be the thing that decides.
func TestNewLogger_SelectiveModeDoesNotVetoWithTheGlobalFloor(t *testing.T) {
	withRestoredLogState(t)

	_ = NewLogger(&cliArgs.ParsedArgs{
		Env:          "development",
		Verbose:      true,
		DebugModules: []string{"probe"},
	})

	if zerolog.GlobalLevel() > zerolog.DebugLevel {
		t.Fatalf("global level is %v: every debug event is dropped before the filter is consulted", zerolog.GlobalLevel())
	}

	st := levelStatePo.Load()
	if !st.selective {
		t.Error("selective mode was not recorded")
	}
	if !isModuleEnabled(st, "probe.veeam") {
		t.Error("prefix filter 'probe' does not match probe.veeam")
	}
}

// The selected module emits even though selective mode deliberately keeps the
// base logger quiet, and an unselected one stays silent.
func TestSelectiveMode_SelectedModuleEmitsThroughAQuietBaseLogger(t *testing.T) {
	withRestoredLogState(t)

	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	mutateLevelState(func(st *levelState) {
		st.selective = true
		st.debugModules = map[string]bool{"probe": true}
	})

	var buf bytes.Buffer
	// Info, as NewLogger leaves it in selective mode.
	base := zerolog.New(&buf).Level(zerolog.InfoLevel)

	NewModuleLogger(&base, "probe.veeam").Debug().Msg("selected")
	if buf.Len() == 0 {
		t.Fatal("the selected module emitted nothing: --filter selects nothing at all")
	}

	buf.Reset()
	NewModuleLogger(&base, "strategy.http").Debug().Msg("not selected")
	if buf.Len() != 0 {
		t.Errorf("an unselected module emitted %q", buf.String())
	}
}

// --verbose alone must reach EVERY module, including the ones added after the
// default level map was written — which is all but sixteen of them.
func TestVerboseMode_ReachesAModuleAbsentFromTheDefaultMap(t *testing.T) {
	withRestoredLogState(t)

	_ = NewLogger(&cliArgs.ParsedArgs{Env: "development", Verbose: true})

	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)

	for _, module := range []string{"probes.swarm", "probes.kubernetes", "probe.veeam", "cache"} {
		buf.Reset()
		NewModuleLogger(&base, module).Debug().Msg("verbose")
		if buf.Len() == 0 {
			t.Errorf("module %q emitted nothing under --verbose", module)
		}
	}
}

// Without any flag, debug stays off — the floor is what enforces it.
func TestNewLogger_QuietByDefault(t *testing.T) {
	withRestoredLogState(t)

	_ = NewLogger(&cliArgs.ParsedArgs{Env: "development"})
	// buildDevelopmentLogger raises the floor for local work; the production
	// path is the one that must stay quiet.
	_ = NewLogger(&cliArgs.ParsedArgs{Env: "production"})

	if zerolog.GlobalLevel() < zerolog.InfoLevel {
		t.Errorf("global level is %v with no verbose flag, want Info or higher", zerolog.GlobalLevel())
	}
}

// An explicit per-module override must beat a quieter base logger, which is
// how the runtime log-level endpoint raises one module on a running agent.
func TestModuleOverride_BeatsAQuieterBaseLogger(t *testing.T) {
	withRestoredLogState(t)

	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	mutateLevelState(func(st *levelState) {
		st.selective = false
		st.levels = map[string]zerolog.Level{"probe.veeam": zerolog.DebugLevel}
	})

	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.InfoLevel)
	NewModuleLogger(&base, "probe.veeam").Debug().Msg("raised at runtime")
	if buf.Len() == 0 {
		t.Fatal("a module raised to debug at runtime emitted nothing")
	}

	// And an override ABOVE debug still silences it.
	mutateLevelState(func(st *levelState) {
		st.levels["probe.veeam"] = zerolog.ErrorLevel
	})
	buf.Reset()
	NewModuleLogger(&base, "probe.veeam").Debug().Msg("should be silenced")
	if buf.Len() != 0 {
		t.Errorf("an error-level override still emitted debug: %q", buf.String())
	}
}
