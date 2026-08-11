package entity

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toise-dev/toise/pkg/emit/wire"
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
	stamped := stampedTagKeys(t)

	for typ, d := range TelemetryContract {
		if d.SubjectKey == "" {
			continue
		}
		// A longer alias is only a defect when the declared key itself reaches
		// no series. Once the probe stamps the subject key directly, the
		// consumer's join works and a differently-named label beside it is
		// redundancy, not a trap — which is the state network.interface is in
		// after #748: `interface` still maps to network.interface.name for the
		// dashboards built on it, while interface.name carries the identity.
		if produced[d.SubjectKey] != nil || stamped[d.SubjectKey] {
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

// stampedTagKeys reports the identity keys probes stamp directly on their
// datapoints, by scanning the probe sources for the key as a string literal.
//
// It exists because the transformer YAML only shows keys a transformer
// RENAMES — a key the probe stamps verbatim (network.device.id, and
// interface.name since #748) never appears there, so reading the YAML alone
// would report a false gap for the types that actually work.
//
// KNOWN LIMIT, stated because it was measured rather than assumed: this scan
// answers "does ANY probe stamp this key", not "does EVERY emitter of this
// entity type stamp it". #748 was exactly the second question — snmppoll
// stamped interface.name while the host probe did not, and both feed the same
// entity type. Removing the host probe's literal does NOT make this test fail,
// because snmppoll's occurrence still satisfies the scan.
//
// So per-emitter coverage is NOT guarded here. It is guarded by the probe's
// own regression (network: TestCollectStampsInterfaceNameForTheEntityJoin),
// which asserts against emitted datapoints, and it is the reason the
// value-equality half of C6 needs a harness that runs a probe cycle rather
// than reading source. Do not add emitters to this scan expecting it to
// notice one of them going quiet.
// The real value check — that the stamped value equals the entity's identity
// — needs a probe cycle and is the other half of C6.
func stampedTagKeys(t *testing.T) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	root := filepath.Join("..", "..", "probes")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		body := string(raw)
		for _, d := range TelemetryContract {
			if d.SubjectKey == "" {
				continue
			}
			if strings.Contains(body, `"`+d.SubjectKey+`"`) {
				out[d.SubjectKey] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning probe sources under %s: %v", root, err)
	}
	return out
}

// The guarantee the SDK vocabulary is supposed to buy: we consume what the
// consumer applies, not a copy kept in parallel.
//
// Toise derives their own ingest registry from the same wire package, with a
// test comparing the two sets in both directions. This is our half of that
// pairing — it fails if the SDK gains a type we have not declared, which is
// how a vocabulary change reaches us as a build error instead of as entities
// silently dropped at their boundary.
func TestVocabularyIsTheSDKs(t *testing.T) {
	sdk := wire.EntityTypes()
	if len(sdk) == 0 {
		t.Fatal("the SDK reports no entity types — the check would pass vacuously")
	}

	declared := make(map[string]bool, len(TelemetryContract))
	for typ := range TelemetryContract {
		declared[typ] = true
	}
	for _, typ := range sdk {
		if !declared[typ] {
			t.Errorf("the SDK registers %q and this contract does not declare it; "+
				"a type the consumer accepts but we never describe has no stated "+
				"telemetry, which is the gap C4 exists to close", typ)
		}
	}
	if len(declared) != len(sdk) {
		t.Errorf("contract declares %d types, the SDK registers %d — the two must "+
			"be the same set, not merely overlap", len(declared), len(sdk))
	}
}
