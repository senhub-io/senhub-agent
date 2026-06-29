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

func TestInstalledServiceUser(t *testing.T) {
	cases := []struct {
		name string
		unit string
		want string
	}{
		{"hardened senhub", "[Service]\nUser=senhub\nGroup=senhub\n", "senhub"},
		{"legacy root explicit", "[Service]\nUser=root\nGroup=root\n", rootServiceUser},
		{"no User= line is root", "[Service]\nExecStart=/usr/local/bin/senhub-agent run\n", rootServiceUser},
		{"empty User= is root", "[Service]\nUser=\n", rootServiceUser},
		{"custom user", "[Service]\nUser=monitor\n", "monitor"},
		{"indented User=", "[Service]\n   User=senhub\n", "senhub"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := installedServiceUser(tc.unit); got != tc.want {
				t.Errorf("installedServiceUser() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicalUnitForUser(t *testing.T) {
	// Default user yields the packaged unit byte-for-byte.
	if got := canonicalUnitForUser(defaultServiceUser); got != packagedSystemdUnit {
		t.Error("canonicalUnitForUser(senhub) must equal packagedSystemdUnit verbatim")
	}

	// Refreshing a root install must NOT reintroduce User=senhub — that
	// is the 217/USER crash-loop the fix prevents (#575).
	root := canonicalUnitForUser(rootServiceUser)
	if !strings.Contains(root, "User=root") || !strings.Contains(root, "Group=root") {
		t.Errorf("root unit must run as root\n%s", root)
	}
	if strings.Contains(root, "User=senhub") || strings.Contains(root, "Group=senhub") {
		t.Errorf("root unit must not reference the senhub user\n%s", root)
	}
	// Every hardening directive of the packaged unit survives — only
	// User=/Group= are re-templated.
	for _, line := range strings.Split(packagedSystemdUnit, "\n") {
		if strings.HasPrefix(line, "User=") || strings.HasPrefix(line, "Group=") {
			continue
		}
		if !strings.Contains(root, line) {
			t.Errorf("root unit lost packaged line %q", line)
		}
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
