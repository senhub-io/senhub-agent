package entity

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The direction that was missing (#753).
//
// TestEveryEntityTypeIsDeclared walks AllTypes and checks each entry has a
// declaration — it confronts the list with the declaration. Nothing confronted
// the CODE with the list, so a type the agent emits that nobody wrote down was
// invisible: `process` and `compute.vm` were emitted as bare literals for
// months while the vocabulary claimed ten types.
//
// It took counting entity types with the consumer to notice. That is the same
// defect shape as #748 — a claim verified against itself — reproduced by the
// very file meant to prevent it.
// Two shapes reach an Entity.Type in this codebase: a literal in the struct
// literal, and a literal assigned to a variable that is then used as the type
// (hyperv does the latter, which is why a Type:-only pattern missed it).
//
// The scan is restricted to entity_source.go files. A broader sweep matches
// every struct field named Type in the tree — metric kinds, PDH counter
// kinds — and drowns the signal.
var entityTypePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)\bType:\s*"([a-z][a-z0-9._]*)"`),
	regexp.MustCompile(`(?m)\b\w*[eE]ntityType\w*\s*:?=\s*"([a-z][a-z0-9._]*)"`),
}

func TestNoEntityTypeIsEmittedOutsideTheVocabulary(t *testing.T) {
	known := make(map[string]bool, len(AllTypes))
	for _, typ := range AllTypes {
		known[typ] = true
	}

	// Relationship literals share the `Type:` field name. They are a separate
	// vocabulary, owned by the same consumer, and are not this test's subject.
	relationTypes := map[string]bool{
		"runs_on": true, "monitors": true, "has_interface": true,
		"has_route": true, "connected_to": true, "bound_to": true,
		"next_hop_via": true, "listens_on": true, "depends_on": true,
		"same_as": true, "routes_via": true, "forwards_to": true,
		"adjacent_to": true,
	}

	type finding struct{ file, value string }
	var unknown []finding
	scanned := 0

	roots := []string{
		filepath.Join("..", "..", "probes"),
		filepath.Join("."),
	}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() ||
				!strings.HasSuffix(path, "entity_source.go") {
				return nil
			}
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			scanned++
			for _, re := range entityTypePatterns {
				for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
					v := m[1]
					if known[v] || relationTypes[v] {
						continue
					}
					unknown = append(unknown, finding{file: path, value: v})
				}
			}
			return nil
		})
	}

	if scanned == 0 {
		t.Fatal("scanned no sources — the test would pass vacuously")
	}
	for _, f := range unknown {
		t.Errorf("%s emits entity type %q, which is not in AllTypes.\n"+
			"  The vocabulary is frozen with the consumer: a type it does not "+
			"register is dropped at the boundary.\n"+
			"  Add it to AllTypes with a C4 declaration, or stop emitting it.",
			f.file, f.value)
	}
}
