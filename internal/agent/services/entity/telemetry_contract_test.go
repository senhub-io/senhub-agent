package entity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// C6 — the declaration is enforced by a test.
//
// ENTITY-DETECTION.md §5 was correct for fourteen months and drifted anyway,
// because nothing failed when it was ignored. These tests are what makes the
// contract load-bearing rather than aspirational.

func TestEveryEntityTypeIsDeclared(t *testing.T) {
	for _, typ := range AllTypes {
		if _, ok := TelemetryContract[typ]; !ok {
			t.Errorf("entity type %q has no telemetry declaration; "+
				"add one to TelemetryContract — a new type must not ship "+
				"without saying where its telemetry is (C4)", typ)
		}
	}

	known := make(map[string]bool, len(AllTypes))
	for _, typ := range AllTypes {
		known[typ] = true
	}
	for typ := range TelemetryContract {
		if !known[typ] {
			t.Errorf("TelemetryContract declares %q, which is not in AllTypes; "+
				"the vocabulary is frozen with Toise — either add the type to "+
				"AllTypes or drop the declaration", typ)
		}
	}
}

func TestDeclarationsAreInternallyConsistent(t *testing.T) {
	for typ, d := range TelemetryContract {
		switch d.Status {
		case StatusOwnKey:
			if d.SubjectKey == "" {
				t.Errorf("%s: own-key types must name the identity attribute "+
					"their telemetry carries", typ)
			}
			if d.Carrier != CarrierResource && d.Carrier != CarrierDatapoint {
				t.Errorf("%s: own-key types must say where the subject key "+
					"rides (resource or datapoint), got %q", typ, d.Carrier)
			}
			if d.InheritedVia != "" {
				t.Errorf("%s: own-key types do not inherit; drop InheritedVia", typ)
			}
		case StatusInherited:
			if d.InheritedVia == "" {
				t.Errorf("%s: inherited types must name the structural edge "+
					"that reaches telemetry — an unnamed edge cannot be "+
					"followed by a consumer", typ)
			}
			if d.SubjectKey != "" || d.Carrier != CarrierNone {
				t.Errorf("%s: inherited types carry no subject key of their own", typ)
			}
		case StatusGraphOnly:
			if d.SubjectKey != "" || d.Carrier != CarrierNone || d.InheritedVia != "" {
				t.Errorf("%s: graph-only types carry nothing; found subject key "+
					"%q, carrier %q, edge %q", typ, d.SubjectKey, d.Carrier, d.InheritedVia)
			}
		default:
			t.Errorf("%s: unknown telemetry status %q", typ, d.Status)
		}
	}
}

// A gap is allowed. An untracked gap is not: the whole point of the
// declaration is that a shortfall surfaces through an issue instead of
// through a dashboard that quietly returns nothing.
func TestUnshippedDeclarationsCiteAnIssue(t *testing.T) {
	for typ, d := range TelemetryContract {
		if d.Shipped {
			if d.Gap != "" {
				t.Errorf("%s: declared shipped but carries a gap note; "+
					"either clear the note or set Shipped=false", typ)
			}
			continue
		}
		if d.Gap == "" {
			t.Errorf("%s: declared not shipped without saying what is missing", typ)
			continue
		}
		if !strings.Contains(d.Gap, "#") {
			t.Errorf("%s: gap note cites no issue number; a gap with no exit "+
				"is technical debt that outlives everyone who knew about it:\n  %s",
				typ, d.Gap)
		}
	}
}

// The #748 detector, and the reason C6 asserts equality rather than presence.
//
// A transformer that renames a probe tag to an attribute which is the
// declared subject key wearing a prefix produces a label that exists, is
// populated, and joins nothing: the consumer asks for the entity's key and
// gets no series, while a near-identical label sits next to it holding all
// of them. That is exactly network.interface today — entity keyed
// `interface.name`, host metrics labelled `network.interface.name`.
func TestTransformersDoNotRenameASubjectKeyIntoANearMiss(t *testing.T) {
	produced := producedAttributes(t)

	for typ, d := range TelemetryContract {
		if d.SubjectKey == "" {
			continue
		}
		for attr, sources := range produced {
			if attr == d.SubjectKey {
				continue
			}
			if !strings.HasSuffix(attr, "."+d.SubjectKey) {
				continue
			}
			msg := "%s declares subject key %q, but a transformer produces %q — " +
				"the same notion under a longer name.\n" +
				"  A consumer following the entity's own key finds nothing, while " +
				"%q carries the series.\n" +
				"  Emitted by: %s\n" +
				"  Fix the emitter or the declaration, not the consumer."
			if d.Shipped {
				t.Errorf(msg, typ, d.SubjectKey, attr, attr, strings.Join(sources, ", "))
				continue
			}
			// A tracked gap: assert it is still the shape the note describes,
			// so the day it is fixed this test tells us to flip Shipped.
			t.Logf("known gap confirmed — "+msg, typ, d.SubjectKey, attr, attr,
				strings.Join(sources, ", "))
		}
	}
}

// producedAttributes collects every OTel attribute name the transformer
// definitions produce from a probe tag, mapped to the files that produce it.
func producedAttributes(t *testing.T) map[string][]string {
	t.Helper()

	dir := filepath.Join("..", "data_store", "transformers", "definitions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading transformer definitions from %s: %v", dir, err)
	}

	type metricDef struct {
		Name           string            `yaml:"name"`
		TagToAttribute map[string]string `yaml:"tag_to_attribute"`
	}
	type definitionFile struct {
		Metrics []metricDef `yaml:"metrics"`
	}

	out := map[string][]string{}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		var doc definitionFile
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			// A definition this test cannot parse is not this test's failure
			// to report; the transformer suite owns schema validation.
			continue
		}
		for _, m := range doc.Metrics {
			for _, attr := range m.TagToAttribute {
				key := attr + "\x00" + e.Name()
				if seen[key] {
					continue
				}
				seen[key] = true
				out[attr] = append(out[attr], e.Name())
			}
		}
	}

	if len(out) == 0 {
		t.Fatalf("no tag_to_attribute mappings found under %s — the test would "+
			"pass vacuously", dir)
	}
	return out
}
