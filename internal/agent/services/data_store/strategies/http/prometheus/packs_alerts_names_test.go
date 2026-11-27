package prometheus

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

type packRule struct {
	Alert string `yaml:"alert"`
	Expr  string `yaml:"expr"`
}

type packGroup struct {
	Rules []packRule `yaml:"rules"`
}

type packFile struct {
	Groups []packGroup `yaml:"groups"`
}

var packTokenRe = regexp.MustCompile(`\b[a-z_][a-z0-9_]*\b`)

// TestAlertPacks_QueryNamesTheAgentEmits checks that every metric name
// used in the vmalert rule packs is a name the agent actually emits,
// and that rules which use a Prometheus-exposition-only name also
// query the corresponding OTLP name when the two differ.
func TestAlertPacks_QueryNamesTheAgentEmits(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "..", "..", "packs", "alerts", "vmalert", "*.rules.yml"))
	if err != nil {
		t.Fatalf("glob rule files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no vmalert rule files found under packs/alerts/vmalert")
	}

	// exposed maps the Prometheus exposition name of every metric the
	// agent emits to the name the OTLP -> VictoriaMetrics path produces
	// for the same metric.
	exposed := map[string]string{}
	defs, err := transformers.DefinitionMetrics()
	if err != nil {
		t.Fatalf("definitions: %v", err)
	}
	for _, d := range slices.Concat(slices.Collect(maps.Values(defs))...) {
		if d.Otel == nil || d.Otel.Skip || d.Otel.Name == "" {
			continue
		}
		promName := OTelNameToPromName(d.Otel.Name, d.Otel.Unit, d.Otel.Type)
		otlpName := promName
		if !strings.HasPrefix(d.Otel.Name, "senhub.") {
			otlpName = strings.TrimPrefix(promName, "senhub_")
		}
		exposed[promName] = otlpName
	}

	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		var pf packFile
		if err := yaml.Unmarshal(raw, &pf); err != nil {
			t.Errorf("decode %s: %v", path, err)
			continue
		}
		for _, g := range pf.Groups {
			for _, r := range g.Rules {
				tokens := map[string]bool{}
				for _, tok := range packTokenRe.FindAllString(r.Expr, -1) {
					tokens[tok] = true
				}
				for _, tok := range packTokenRe.FindAllString(r.Expr, -1) {
					if !strings.HasPrefix(tok, "senhub_") || strings.HasPrefix(tok, "senhub_agent_") {
						continue
					}
					otlpName, ok := exposed[tok]
					if !ok {
						t.Errorf("rule %s queries %s, which the agent does not emit", r.Alert, tok)
						continue
					}
					if otlpName != tok && !tokens[otlpName] {
						t.Errorf("rule %s misses the OTLP name %s", r.Alert, otlpName)
					}
				}
			}
		}
	}
}
