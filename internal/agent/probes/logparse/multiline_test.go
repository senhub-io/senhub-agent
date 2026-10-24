package logparse

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func mlConfig(t *testing.T, pattern, match string, negate bool) MultilineConfig {
	t.Helper()
	cfg := MultilineConfig{Pattern: pattern, Match: match, Negate: negate}
	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		cfg.compiled = re
	}
	return cfg
}

func TestMultiline_DisabledPassThrough(t *testing.T) {
	a := NewAssembler(MultilineConfig{}, 0)
	got := a.Append("a single line")
	if !reflect.DeepEqual(got, []string{"a single line"}) {
		t.Errorf("got %v", got)
	}
	if a.Flush() != nil {
		t.Errorf("flush on disabled should be nil")
	}
}

func TestMultiline_AfterFoldsStacktrace(t *testing.T) {
	a := NewAssembler(mlConfig(t, `^\d{4}-`, "after", false), 0)

	var out []string
	feed := []string{
		"2026-06-01 ERROR boom",
		"  at foo()",
		"  at bar()",
		"2026-06-01 INFO next",
	}
	for _, l := range feed {
		out = append(out, a.Append(l)...)
	}
	out = append(out, a.Flush()...)

	want := []string{
		"2026-06-01 ERROR boom\n  at foo()\n  at bar()",
		"2026-06-01 INFO next",
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v\nwant %v", out, want)
	}
}

func TestMultiline_OrphanContinuationEmittedAlone(t *testing.T) {
	a := NewAssembler(mlConfig(t, `^START`, "after", false), 0)
	// File opens mid-message: first line is a continuation with nothing
	// started — must not be swallowed.
	got := a.Append("  trailing fragment")
	if !reflect.DeepEqual(got, []string{"  trailing fragment"}) {
		t.Errorf("orphan got %v", got)
	}
}

func TestMultiline_BeforeFlushesOnMatch(t *testing.T) {
	// "before": the matching line is the LAST line of a record.
	a := NewAssembler(mlConfig(t, `END$`, "before", false), 0)
	var out []string
	for _, l := range []string{"line1", "line2", "tail END"} {
		out = append(out, a.Append(l)...)
	}
	want := []string{"line1\nline2\ntail END"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v want %v", out, want)
	}
}

func TestMultiline_Negate(t *testing.T) {
	// Negate inverts: a line NOT matching the pattern starts a new record.
	a := NewAssembler(mlConfig(t, `^\s`, "after", true), 0)
	var out []string
	for _, l := range []string{"head one", "  cont", "head two"} {
		out = append(out, a.Append(l)...)
	}
	out = append(out, a.Flush()...)
	want := []string{"head one\n  cont", "head two"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v want %v", out, want)
	}
}

func TestMultiline_TruncatesAtMaxLen(t *testing.T) {
	a := NewAssembler(mlConfig(t, `^X`, "after", false), 5)
	a.Append("Xabcdefghij")
	got := a.Flush()
	if len(got) != 1 || len(got[0]) != 5 {
		t.Errorf("expected truncation to 5 bytes, got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if Truncate("hello", 0) != "hello" {
		t.Errorf("max=0 should not truncate")
	}
	if Truncate("hello", 3) != "hel" {
		t.Errorf("got %q", Truncate("hello", 3))
	}
}

// A PRTG-style log read with the Filebeat spelling of "a timestamp starts
// a record" (negate: true). Under this assembler that spelling means the
// reverse: the first line without a timestamp opens a record, and every
// timestamped line after it is folded into that record until the next
// line without one.
func TestMultiline_FilebeatNegateHoldsTimestampedLines(t *testing.T) {
	lines := []string{
		"2026-10-20 09:29:58.123456 INFO  TId 1234 Core> sensor scan done\r",
		"2026-10-20 09:30:01.000001 ERROR TId 1234 Core> request failed:\r",
		"   at Paessler.Core.Request()",
		"2026-10-20 09:30:02.000002 INFO  TId 1235 Core> next message",
		"2026-10-20 09:30:03.000003 INFO  TId 1236 Core> another message",
	}
	feed := func(a *Assembler) []string {
		var out []string
		for _, l := range lines {
			out = append(out, a.Append(strings.TrimSuffix(l, "\r"))...)
		}
		return out
	}

	filebeat := NewAssembler(mlConfig(t, `^\d{4}-\d{2}-\d{2} `, "after", true), 0)
	got := feed(filebeat)
	if len(got) != 2 {
		t.Fatalf("negate: true emitted %d records before the flush, want the 2 lines ahead of the continuation: %q", len(got), got)
	}
	held := filebeat.Flush()
	if len(held) != 1 || strings.Count(held[0], "\n") != 2 || !strings.HasPrefix(held[0], "   at ") {
		t.Fatalf("negate: true should hold the continuation with the two later timestamped lines, got %q", held)
	}

	header := NewAssembler(mlConfig(t, `^\d{4}-\d{2}-\d{2} `, "after", false), 0)
	got = append(feed(header), header.Flush()...)
	want := []string{
		"2026-10-20 09:29:58.123456 INFO  TId 1234 Core> sensor scan done",
		"2026-10-20 09:30:01.000001 ERROR TId 1234 Core> request failed:\n   at Paessler.Core.Request()",
		"2026-10-20 09:30:02.000002 INFO  TId 1235 Core> next message",
		"2026-10-20 09:30:03.000003 INFO  TId 1236 Core> another message",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("negate: false\n got %q\nwant %q", got, want)
	}
}

// A record that reaches the cap is sent, and the lines after it are kept
// as records of their own instead of being dropped until the next record
// starts.
func TestMultiline_FullRecordIsSentAndTheNextLineKept(t *testing.T) {
	a := NewAssembler(mlConfig(t, `^X`, "after", false), 14)
	var out []string
	for _, l := range []string{"Xhead", " cont1", " cont2", " cont3"} {
		out = append(out, a.Append(l)...)
	}
	out = append(out, a.Flush()...)
	want := []string{"Xhead\n cont1", " cont2\n cont3"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestMultilineWarning(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		match   string
		negate  bool
		want    bool
	}{
		{"Filebeat spelling of a leading timestamp", `^\d{4}-\d{2}-\d{2} `, "after", true, true},
		{"leading timestamp written for this agent", `^\d{4}-\d{2}-\d{2} `, "after", false, false},
		{"negated continuation pattern", `\s+at `, "after", true, false},
		{"negated match before", `^\d{4}`, "before", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MultilineWarning(mlConfig(t, tt.pattern, tt.match, tt.negate))
			if (got != "") != tt.want {
				t.Fatalf("MultilineWarning = %q, want a warning %v", got, tt.want)
			}
			if tt.want && !strings.Contains(got, "negate: false") {
				t.Fatalf("warning %q should give the line to write", got)
			}
		})
	}
}
