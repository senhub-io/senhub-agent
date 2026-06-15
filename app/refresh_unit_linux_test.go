//go:build linux

package app

import (
	"strings"
	"testing"
)

func TestDiffLines_Identical(t *testing.T) {
	s := "a\nb\nc\n"
	got := diffLines(s, s)
	if len(got) != 0 {
		t.Errorf("diffLines(s, s) = %v, want nil or empty", got)
	}
}

func TestDiffLines_AddedLine(t *testing.T) {
	old := "a\n"
	new := "a\nb\n"
	got := diffLines(old, new)
	hasPlus := false
	hasMinus := false
	for _, l := range got {
		if strings.HasPrefix(l, "+ b") {
			hasPlus = true
		}
		if strings.HasPrefix(l, "- ") {
			hasMinus = true
		}
	}
	if !hasPlus {
		t.Errorf("expected '+ b' in diff output, got %v", got)
	}
	if hasMinus {
		t.Errorf("unexpected removal line in diff output, got %v", got)
	}
}

func TestDiffLines_RemovedLine(t *testing.T) {
	old := "a\nb\n"
	new := "a\n"
	got := diffLines(old, new)
	hasMinus := false
	hasPlus := false
	for _, l := range got {
		if strings.HasPrefix(l, "- b") {
			hasMinus = true
		}
		if strings.HasPrefix(l, "+ ") {
			hasPlus = true
		}
	}
	if !hasMinus {
		t.Errorf("expected '- b' in diff output, got %v", got)
	}
	if hasPlus {
		t.Errorf("unexpected addition line in diff output, got %v", got)
	}
}

func TestDiffLines_ChangedLine(t *testing.T) {
	old := "a\n"
	new := "c\n"
	got := diffLines(old, new)
	hasMinus := false
	hasPlus := false
	for _, l := range got {
		if strings.HasPrefix(l, "- a") {
			hasMinus = true
		}
		if strings.HasPrefix(l, "+ c") {
			hasPlus = true
		}
	}
	if !hasMinus {
		t.Errorf("expected '- a' in diff output, got %v", got)
	}
	if !hasPlus {
		t.Errorf("expected '+ c' in diff output, got %v", got)
	}
}

func TestDiffLines_ContextLines(t *testing.T) {
	old := "line1\nline2\nold\nline4\nline5\n"
	new := "line1\nline2\nnew\nline4\nline5\n"
	got := diffLines(old, new)
	// line1/line2 are context before the change, line4/line5 after.
	hasContext := false
	for _, l := range got {
		if strings.HasPrefix(l, "  line") {
			hasContext = true
			break
		}
	}
	if !hasContext {
		t.Errorf("expected context lines with '  ' prefix in diff output, got %v", got)
	}
}
