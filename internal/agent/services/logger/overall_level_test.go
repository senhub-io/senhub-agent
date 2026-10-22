package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestOverallLevelGovernsEveryModuleUntilCleared(t *testing.T) {
	t.Cleanup(ClearOverallLevel)
	var buf bytes.Buffer
	zl := zerolog.New(&buf).Level(zerolog.InfoLevel)
	m := NewModuleLogger((*Logger)(&zl), "probe.overall-test")

	write := func() string {
		buf.Reset()
		m.Debug().Msg("d")
		m.Info().Msg("i")
		m.Warn().Msg("w")
		m.Error().Msg("e")
		return buf.String()
	}
	levels := func(out string) string {
		var got []string
		for _, l := range []string{"debug", "info", "warn", "error"} {
			if strings.Contains(out, `"level":"`+l+`"`) {
				got = append(got, l)
			}
		}
		return strings.Join(got, ",")
	}

	if got := levels(write()); got != "info,warn,error" {
		t.Fatalf("before: %s", got)
	}
	if err := SetOverallLevel("debug"); err != nil {
		t.Fatal(err)
	}
	if got := levels(write()); got != "debug,info,warn,error" {
		t.Errorf("debug: %s", got)
	}
	if err := SetOverallLevel("warn"); err != nil {
		t.Fatal(err)
	}
	if got := levels(write()); got != "warn,error" {
		t.Errorf("warn: %s", got)
	}
	ClearOverallLevel()
	if got := levels(write()); got != "info,warn,error" {
		t.Errorf("cleared: %s", got)
	}
	if err := SetOverallLevel("trace"); err == nil {
		t.Error("a level outside debug, info, warn and error must be refused")
	}
}
