package strategyreg

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store"
)

// blockSource names the function that reads one declared sub-block and
// the map variable it reads it through. File paths are relative to
// ../strategies/.
type blockSource struct {
	file     string
	function string
	variable string
	// tableKeys also collects the string keys of the composite literals
	// in the function ({"ca_file", &cfg.CAFile} or "ca_file": &cfg.CAFile),
	// for a parser that loops over a key table instead of indexing literals.
	tableKeys bool
}

// blockSources ties every declared (strategy, block) to the code that
// reads it. The guard test parses the named function and collects its
// `<variable>["literal"]` lookups, then checks both directions against
// the declaration in register.go.
var blockSources = map[string][]blockSource{
	"otlp/tls":                 {{"otlp/config.go", "parseTLS", "m", false}},
	"otlp/retry":               {{"otlp/config.go", "parseRetry", "m", false}},
	"otlp/persistence":         {{"otlp/config.go", "parsePersistence", "m", false}},
	"otlp/memory_limit":        {{"otlp/config.go", "parseMemoryLimit", "m", false}},
	"otlp/relay":               {{"otlp/config.go", "parseRelay", "m", false}},
	"otlp/signals.metrics":     {{"otlp/config.go", "parseSignals", "mm", false}, {"otlp/config.go", "parseSignalTransport", "m", false}},
	"otlp/signals.logs":        {{"otlp/config.go", "parseSignals", "lm", false}, {"otlp/config.go", "parseSignalTransport", "m", false}},
	"otlp/signals.traces":      {{"otlp/config.go", "parseSignals", "tm", false}, {"otlp/config.go", "parseSignalTransport", "m", false}},
	"otlp/signals.entities":    {{"otlp/config.go", "parseSignals", "em", false}},
	"otlp/signals.metrics.tls": {{"otlp/config.go", "parseTLS", "m", false}},
	"otlp/signals.logs.tls":    {{"otlp/config.go", "parseTLS", "m", false}},
	"otlp/signals.traces.tls":  {{"otlp/config.go", "parseTLS", "m", false}},
	"zabbix/passive":           {{"zabbix/config.go", "parsePassive", "block", false}},
	"zabbix/tls":               {{"zabbix/config.go", "parseTLS", "block", true}},
	"zabbix/passive.tls":       {{"zabbix/config.go", "parsePassiveTLS", "block", true}},
}

// untraceableBlocks holds declared blocks the table cannot express,
// with the reason. http's three blocks are read by the strategy's
// configuration manager rather than a single function holding one map
// variable, and their keys are already covered by
// TestKnownParamsCoverEveryKeyTheStrategiesRead, which scans the whole
// strategy directory for map lookups.
var untraceableBlocks = map[string]string{
	"http/prtg":       "reader is the http strategy's ConfigurationManager.parseParams; keys covered by the top-level guard",
	"http/prometheus": "reader is the http strategy's ConfigurationManager.parseParams; keys covered by the top-level guard",
	"http/tls":        "reader is the http strategy's ConfigurationManager.parseParams; keys covered by the top-level guard",
}

// declaredWithoutReader lists declared keys no traced source reads,
// with the reason. These are keys that ARE read, just through a path
// the table cannot express.
var declaredWithoutReader = map[string]string{
	// Read as m[q.signal+"_queue_max_bytes"] — a concatenation the
	// literal collector cannot see.
	"otlp/persistence/metrics_queue_max_bytes": "read via q.signal+\"_queue_max_bytes\" concatenation",
	"otlp/persistence/metrics_queue_max_age":   "read via q.signal+\"_queue_max_age\" concatenation",
	"otlp/persistence/traces_queue_max_bytes":  "read via q.signal+\"_queue_max_bytes\" concatenation",
	"otlp/persistence/traces_queue_max_age":    "read via q.signal+\"_queue_max_age\" concatenation",
}

// readWithoutDeclaration lists keys a traced source reads that are
// deliberately not declared, with the reason.
var readWithoutDeclaration = map[string]string{}

// TestKnownBlockParamsMatchTheCode keeps the sub-block declarations
// honest in both directions: a key a parser reads that is missing from
// the declaration would make `agent config check` reject a working
// file, and a declared key no parser reads would let a typo through
// silently — the exact silence #846 removed, one level deeper.
func TestKnownBlockParamsMatchTheCode(t *testing.T) {
	declaredIDs := map[string]bool{}

	for _, name := range data_store.StrategiesDeclaringParams() {
		for block, keys := range data_store.KnownBlocksFor(name) {
			id := name + "/" + block
			declaredIDs[id] = true

			if _, excluded := untraceableBlocks[id]; excluded {
				continue
			}
			sources, ok := blockSources[id]
			if !ok {
				t.Errorf("block %s is declared but has no entry in blockSources; the guard cannot tie it to a reader", id)
				continue
			}

			read := map[string]struct{}{}
			for _, src := range sources {
				keys := keysReadBySource(t, src)
				if len(keys) == 0 {
					t.Errorf("block %s: %s in %s reads no key through %q — the table no longer matches the code",
						id, src.function, src.file, src.variable)
				}
				for key := range keys {
					read[key] = struct{}{}
				}
			}

			declared := map[string]struct{}{}
			for _, key := range keys {
				declared[key] = struct{}{}
			}

			var missing []string
			for key := range read {
				if _, ok := declared[key]; ok {
					continue
				}
				if _, tolerated := readWithoutDeclaration[id+"/"+key]; tolerated {
					continue
				}
				missing = append(missing, key)
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				t.Errorf("block %s is read with keys %v but does not declare them; config check will reject a working file",
					id, missing)
			}

			var unread []string
			for key := range declared {
				if _, ok := read[key]; ok {
					continue
				}
				if _, tolerated := declaredWithoutReader[id+"/"+key]; tolerated {
					continue
				}
				unread = append(unread, key)
			}
			if len(unread) > 0 {
				sort.Strings(unread)
				t.Errorf("block %s declares keys %v that no traced reader reads; a typo in them passes silently",
					id, unread)
			}
		}
	}

	for id := range blockSources {
		if !declaredIDs[id] {
			t.Errorf("blockSources has an entry for %s but no such block is declared; the table is stale", id)
		}
	}
	for id := range untraceableBlocks {
		if !declaredIDs[id] {
			t.Errorf("untraceableBlocks has an entry for %s but no such block is declared; the exclusion is stale", id)
		}
	}
}

// keysReadBySource parses the named function and collects the keys it
// reads as `<variable>["literal"]` from its body.
func keysReadBySource(t *testing.T, src blockSource) map[string]struct{} {
	t.Helper()

	path := filepath.Join("..", "strategies", filepath.FromSlash(src.file))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if d, ok := decl.(*ast.FuncDecl); ok && d.Name.Name == src.function {
			fn = d
			break
		}
	}
	if fn == nil || fn.Body == nil {
		t.Fatalf("function %s not found in %s", src.function, path)
	}

	keys := map[string]struct{}{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if src.tableKeys {
			if lit, ok := n.(*ast.CompositeLit); ok && len(lit.Elts) > 0 {
				first := lit.Elts[0]
				if _, ok := first.(*ast.KeyValueExpr); ok {
					for _, elt := range lit.Elts {
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							addStringLit(keys, kv.Key)
						}
					}
				} else {
					addStringLit(keys, first)
				}
			}
		}
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		ident, ok := idx.X.(*ast.Ident)
		if !ok || ident.Name != src.variable {
			return true
		}
		lit, ok := idx.Index.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		key, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		keys[key] = struct{}{}
		return true
	})
	return keys
}

// TestOTLPSubBlockTypoIsReported runs the real otlp declarations: a
// complete, valid configuration reports nothing, and a misspelt key two
// levels down is reported with its full path.
func TestOTLPSubBlockTypoIsReported(t *testing.T) {
	valid := map[string]interface{}{
		"tls":   map[string]interface{}{"enabled": true, "ca_file": "/ca.pem"},
		"retry": map[string]interface{}{"enabled": true, "max_interval": "30s"},
		"signals": map[string]interface{}{
			"metrics": map[string]interface{}{"enabled": true, "interval": "10s"},
			"logs": map[string]interface{}{
				"enabled": true,
				"tls":     map[string]interface{}{"enabled": true, "ca_file": "/ca.pem"},
			},
			"traces": map[string]interface{}{
				"enabled":                true,
				"relay_tenant_overrides": []interface{}{map[string]interface{}{"tags": map[string]interface{}{"tenant": "b"}}},
			},
		},
	}
	if got := data_store.UnreadBlockParamsFor("otlp", valid); len(got) != 0 {
		t.Errorf("valid otlp configuration reported unread keys %v", got)
	}

	typo := map[string]interface{}{
		"signals": map[string]interface{}{
			"logs": map[string]interface{}{
				"tls": map[string]interface{}{"enabled": true, "ca_fiel": "/ca.pem"},
			},
		},
	}
	got := data_store.UnreadBlockParamsFor("otlp", typo)
	if len(got) != 1 || got[0] != "signals.logs.tls.ca_fiel" {
		t.Errorf("UnreadBlockParamsFor = %v, want [signals.logs.tls.ca_fiel]", got)
	}
}

func addStringLit(keys map[string]struct{}, expr ast.Expr) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return
	}
	if key, err := strconv.Unquote(lit.Value); err == nil {
		keys[key] = struct{}{}
	}
}
