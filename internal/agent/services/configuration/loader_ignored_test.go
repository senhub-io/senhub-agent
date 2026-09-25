package configuration

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A copy taken before an edit is not loaded, and is named so the
// operator knows. Reported on a host with four otlp.yaml.bak-<date>
// copies in strategies.d, read at the time as if they had been loaded.
func TestACopyBesideAFragmentIsNotLoadedAndIsNamed(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"otlp.yaml", "otlp.yaml.bak-loc-20260908T093218", "20-http.yml", "old.yaml.disabled", ".swp"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("otlp:\n  endpoint: x:4317\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := listYAMLFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("loaded %v, want otlp.yaml and 20-http.yml only", files)
	}
	if got := ignoredFragments(dir); !reflect.DeepEqual(got, []string{"otlp.yaml.bak-loc-20260908T093218"}) {
		t.Errorf("ignored = %v, want the backup copy alone", got)
	}
}
