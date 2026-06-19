//go:build linux

package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsDMIPlaceholder(t *testing.T) {
	reject := []string{
		"", "To Be Filled By O.E.M.", "to be filled by o.e.m.",
		"System manufacturer", "System Product Name", "System Serial Number",
		"Default string", "Not Specified", "None", "Unknown", "n/a",
	}
	for _, v := range reject {
		if !isDMIPlaceholder(v) {
			t.Errorf("isDMIPlaceholder(%q) = false, want true", v)
		}
	}
	keep := []string{"Dell Inc.", "PowerEdge R750", "CZ12345", "LENOVO"}
	for _, v := range keep {
		if isDMIPlaceholder(v) {
			t.Errorf("isDMIPlaceholder(%q) = true, want false", v)
		}
	}
}

func TestReadDMI(t *testing.T) {
	dir := t.TempDir()

	// Trailing newline (as sysfs returns) is trimmed.
	good := filepath.Join(dir, "sys_vendor")
	if err := os.WriteFile(good, []byte("Dell Inc.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readDMI(good); got != "Dell Inc." {
		t.Errorf("readDMI(good) = %q, want %q", got, "Dell Inc.")
	}

	// Placeholder content is dropped.
	ph := filepath.Join(dir, "product_serial")
	if err := os.WriteFile(ph, []byte("System Serial Number\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readDMI(ph); got != "" {
		t.Errorf("readDMI(placeholder) = %q, want empty", got)
	}

	// Missing file (e.g. non-root or no DMI) degrades to empty.
	if got := readDMI(filepath.Join(dir, "absent")); got != "" {
		t.Errorf("readDMI(absent) = %q, want empty", got)
	}
}
