package transformers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestEmbeddedYAMLDecodesToTheSameValues pins what every embedded definition,
// shared table and corrections file decodes to, so a change of YAML library
// shows up as a changed line here rather than as a changed metric name.
// Run with UPDATE_YAML_GOLDEN=1 to rewrite the golden file.
func TestEmbeddedYAMLDecodesToTheSameValues(t *testing.T) {
	tr := NewTransformerRegistry(createTestLogger())

	var lines []string
	digest := func(name string, v interface{}, err error) {
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s ERR %v", name, err))
			return
		}
		body, jerr := json.Marshal(v)
		if jerr != nil {
			lines = append(lines, fmt.Sprintf("%s JSONERR %v", name, jerr))
			return
		}
		lines = append(lines, fmt.Sprintf("%s %x %d", name, sha256.Sum256(body), len(body)))
	}

	walkErr := fs.WalkDir(definitionFiles, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		switch {
		case p == "definitions/shared/units.yaml":
			v, err := tr.loadUnitsConfigFromEmbed(p)
			digest(p, v, err)
		case p == "definitions/shared/templates.yaml":
			v, err := tr.loadTemplatesConfigFromEmbed(p)
			digest(p, v, err)
		case strings.HasPrefix(p, "definitions/shared/"):
			lines = append(lines, p+" UNCOVERED")
		case strings.HasPrefix(p, "definitions/"):
			v, err := tr.loadProbeDefinitionFromEmbed(p)
			digest(p, v, err)
		case strings.HasPrefix(p, "corrections/"):
			name := strings.TrimSuffix(path.Base(p), ".yaml")
			v, err := tr.loadCorrectionsConfigFromEmbed(name)
			digest(p, v, err)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}

	defs, err := Definitions()
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		d := defs[n]
		digest("Definitions()/"+n, &d, nil)
	}
	if err != nil {
		lines = append(lines, "Definitions() ERR "+err.Error())
	}

	sort.Strings(lines)
	assertYAMLGolden(t, "embedded-yaml.golden", strings.Join(lines, "\n")+"\n")
}

func assertYAMLGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_YAML_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(want) != got {
		wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
		for i := 0; i < len(wl) || i < len(gl); i++ {
			var w, g string
			if i < len(wl) {
				w = wl[i]
			}
			if i < len(gl) {
				g = gl[i]
			}
			if w != g {
				t.Fatalf("%s differs at line %d:\n want %q\n  got %q", name, i+1, w, g)
			}
		}
	}
}
