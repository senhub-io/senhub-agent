package filetail

import (
	"reflect"
	"regexp"
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
	a := newMultilineAssembler(MultilineConfig{}, 0)
	got := a.Append("a single line")
	if !reflect.DeepEqual(got, []string{"a single line"}) {
		t.Errorf("got %v", got)
	}
	if a.Flush() != nil {
		t.Errorf("flush on disabled should be nil")
	}
}

func TestMultiline_AfterFoldsStacktrace(t *testing.T) {
	a := newMultilineAssembler(mlConfig(t, `^\d{4}-`, "after", false), 0)

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
	a := newMultilineAssembler(mlConfig(t, `^START`, "after", false), 0)
	// File opens mid-message: first line is a continuation with nothing
	// started — must not be swallowed.
	got := a.Append("  trailing fragment")
	if !reflect.DeepEqual(got, []string{"  trailing fragment"}) {
		t.Errorf("orphan got %v", got)
	}
}

func TestMultiline_BeforeFlushesOnMatch(t *testing.T) {
	// "before": the matching line is the LAST line of a record.
	a := newMultilineAssembler(mlConfig(t, `END$`, "before", false), 0)
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
	a := newMultilineAssembler(mlConfig(t, `^\s`, "after", true), 0)
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
	a := newMultilineAssembler(mlConfig(t, `^X`, "after", false), 5)
	a.Append("Xabcdefghij")
	got := a.Flush()
	if len(got) != 1 || len(got[0]) != 5 {
		t.Errorf("expected truncation to 5 bytes, got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 0) != "hello" {
		t.Errorf("max=0 should not truncate")
	}
	if truncate("hello", 3) != "hel" {
		t.Errorf("got %q", truncate("hello", 3))
	}
}
