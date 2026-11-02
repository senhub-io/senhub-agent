package operstate

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestAnUnmappedValueIsReportedOncePerValue(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = prev })

	FromIFMIB(42)
	FromIFMIB(42)
	FromLinuxSysfs("weird")
	FromLinuxSysfs("")

	out := buf.String()
	if n := strings.Count(out, `"value":"42"`); n != 1 {
		t.Fatalf("value 42 reported %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, `"value":"weird"`); n != 1 {
		t.Fatalf("value weird reported %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, "\n"); n != 2 {
		t.Fatalf("%d lines, want 2 (an empty sysfs value is not a value):\n%s", n, out)
	}
}

func TestUnmappedReportsAreBounded(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = prev })

	for v := 1000; v < 1000+3*maxUnmapped; v++ {
		FromIFMIB(v)
	}
	unmapped.Lock()
	n := len(unmapped.seen)
	unmapped.Unlock()
	if n > maxUnmapped {
		t.Fatalf("%d values remembered, want at most %d", n, maxUnmapped)
	}
	if strings.Count(buf.String(), "are not reported") != 1 {
		t.Fatalf("the cap must be reported exactly once:\n%s", buf.String())
	}
}
