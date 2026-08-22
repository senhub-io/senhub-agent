package docscoverage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// scanRoots are the trees where operator-facing configuration is parsed:
// probe params and strategy params. The loader's own structs are not
// here — their keys are struct fields with yaml tags, a different shape,
// and a separate check if it ever proves necessary.
var scanRoots = []string{
	"internal/agent/probes",
	"internal/agent/services/data_store/strategies",
}

// docRoot is the reference tree an operator is pointed at. A key
// mentioned only in a release note, a developer-guide page or a code
// comment is a key nobody configuring the agent will find.
const docRoot = "docs/user-guide/docs"

// configKeyRe bounds what counts as a key: snake_case, and long enough
// that a map lookup like m["id"] does not enter the corpus as one.
var configKeyRe = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// allowed lists keys that are parsed and deliberately absent from the
// reference. Every entry carries the reason it is not a gap; an entry
// that stops being needed fails the test, so the list cannot rot into a
// place where gaps go to be forgotten.
var allowed = map[string]string{
	"app_name": "not a configuration key — a field of a parsed RFC 5424 syslog message, read out of the map the parser returns",
}

type keySite struct {
	file string
	line int
}

// TestEveryParsedConfigKeyIsDocumented walks the parsers, collects the
// keys they read out of an operator's configuration, and fails on any
// that the user guide never mentions.
func TestEveryParsedConfigKeyIsDocumented(t *testing.T) {
	root := moduleRoot(t)
	keys := collectConfigKeys(t, root)
	if len(keys) < 100 {
		// The collector is a static analysis of an idiom. If a refactor
		// changes how params are read, it would quietly find nothing and
		// this test would pass while checking nothing at all.
		t.Fatalf("found only %d configuration keys — the collector has stopped recognising how params are read", len(keys))
	}

	corpus := documentationCorpus(t, root)

	var gaps []string
	for key, sites := range keys {
		if _, ok := allowed[key]; ok {
			continue
		}
		if strings.Contains(corpus, key) {
			continue
		}
		s := sites[0]
		gaps = append(gaps, "  "+key+"  parsed at "+s.file+":"+strconv.Itoa(s.line))
	}
	sort.Strings(gaps)

	if len(gaps) > 0 {
		t.Errorf(`%d configuration key(s) the agent parses appear nowhere under %s:

%s

Each is a shipped option no operator can discover. Document it on the
page that describes its block, or — if it is not an operator-facing key
at all — add it to `+"`allowed`"+` in this file with the reason why.`,
			len(gaps), docRoot, strings.Join(gaps, "\n"))
	}
}

// TestAllowListHasNoStaleEntries keeps the escape hatch honest: an entry
// for a key that is no longer parsed, or that has since been documented,
// is removed rather than left as cover for the next one.
func TestAllowListHasNoStaleEntries(t *testing.T) {
	root := moduleRoot(t)
	keys := collectConfigKeys(t, root)
	corpus := documentationCorpus(t, root)

	for key, reason := range allowed {
		if reason == "" {
			t.Errorf("allow-list entry %q carries no reason", key)
		}
		if _, parsed := keys[key]; !parsed {
			t.Errorf("allow-list entry %q is no longer parsed anywhere — remove it", key)
			continue
		}
		if strings.Contains(corpus, key) {
			t.Errorf("allow-list entry %q is documented now — remove it, the check covers it", key)
		}
	}
}

// collectConfigKeys finds the string literals used as configuration keys.
//
// It does not look for a named helper or a named variable, because both
// change: it starts from the parameter that carries the operator's
// configuration — a map[string]interface{} — and follows it. A key is
// read either by indexing that map, or by handing the map and a literal
// to a helper. Nested blocks are followed the same way: a map extracted
// out of a tracked map is itself tracked, so `signals.logs.batch_size`
// is found through the two type assertions that reach it.
func collectConfigKeys(t *testing.T, root string) map[string][]keySite {
	t.Helper()

	keys := map[string][]keySite{}
	fset := token.NewFileSet()

	// Per package, not per file: a block reached through a helper —
	// `m := readStringKeyedMap(raw)`, the shape most of the nested OTLP
	// blocks use — is only recognisable once that helper's return type
	// is known, and the helper usually lives in another file of the same
	// package.
	for _, scanRoot := range scanRoots {
		dir := filepath.Join(root, scanRoot)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return nil
			}
			parsed, parseErr := parseDirectory(fset, path)
			if parseErr != nil {
				return parseErr
			}
			mapReturners := configMapReturners(parsed)
			for filePath, file := range parsed {
				rel, relErr := filepath.Rel(root, filePath)
				if relErr != nil {
					rel = filePath
				}
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Body == nil {
						continue
					}
					for key, pos := range keysInFunc(fn, mapReturners) {
						keys[key] = append(keys[key], keySite{file: rel, line: fset.Position(pos).Line})
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	return keys
}

// parseDirectory parses the non-test Go files of one directory. Build
// tags are deliberately ignored: a key parsed only on Windows needs
// documenting just the same.
func parseDirectory(fset *token.FileSet, dir string) (map[string]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		out[path] = file
	}
	return out, nil
}

// configMapReturners names the functions of a package whose first result
// is a string-keyed map. A local assigned from one of them holds a
// configuration block, whatever the shape it arrived in.
func configMapReturners(files map[string]*ast.File) map[string]bool {
	out := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
				continue
			}
			if isConfigMapType(fn.Type.Results.List[0].Type) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// keysInFunc returns the configuration keys read inside one function.
func keysInFunc(fn *ast.FuncDecl, mapReturners map[string]bool) map[string]token.Pos {
	tracked := map[string]bool{}
	if fn.Type.Params != nil {
		for _, param := range fn.Type.Params.List {
			if !isConfigMapType(param.Type) {
				continue
			}
			for _, name := range param.Names {
				tracked[name.Name] = true
			}
		}
	}
	// A function may receive the block as an interface{} and turn it
	// into a map with a package helper. Seed from that too, or every
	// nested OTLP block goes unchecked.
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		callee, ok := call.Fun.(*ast.Ident)
		if !ok || !mapReturners[callee.Name] {
			return true
		}
		if ident, ok := assign.Lhs[0].(*ast.Ident); ok && ident.Name != "_" {
			tracked[ident.Name] = true
		}
		return true
	})

	if len(tracked) == 0 {
		return nil
	}

	// A nested block is reached through an assignment, and an assignment
	// can appear before or after the one it depends on depending on how
	// the function is written. Iterate to a fixpoint rather than assume
	// source order.
	for added := true; added; {
		added = false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Rhs) != 1 {
				return true
			}
			if !readsTrackedMap(assign.Rhs[0], tracked) {
				return true
			}
			for _, lhs := range assign.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok || ident.Name == "_" || tracked[ident.Name] {
					continue
				}
				tracked[ident.Name] = true
				added = true
			}
			return true
		})
	}

	found := map[string]token.Pos{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IndexExpr:
			ident, ok := node.X.(*ast.Ident)
			if !ok || !tracked[ident.Name] {
				return true
			}
			if key, ok := stringLiteral(node.Index); ok {
				record(found, key, node.Index.Pos())
			}

		case *ast.CallExpr:
			// A helper that takes the map and the key: types.IntParam(
			// config, "timeout"). Recognised by shape, so a new helper
			// needs no change here.
			carriesMap := false
			for _, arg := range node.Args {
				if ident, ok := arg.(*ast.Ident); ok && tracked[ident.Name] {
					carriesMap = true
					break
				}
			}
			if !carriesMap {
				return true
			}
			for _, arg := range node.Args {
				if key, ok := stringLiteral(arg); ok {
					record(found, key, arg.Pos())
				}
			}
		}
		return true
	})
	return found
}

func record(found map[string]token.Pos, key string, pos token.Pos) {
	if !configKeyRe.MatchString(key) || len(key) < 3 {
		return
	}
	if _, seen := found[key]; !seen {
		found[key] = pos
	}
}

// readsTrackedMap reports whether an expression pulls a value out of a
// map already known to carry configuration — directly, or through the
// type assertion that turns a nested block into a map again.
func readsTrackedMap(expr ast.Expr, tracked map[string]bool) bool {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		ident, ok := e.X.(*ast.Ident)
		return ok && tracked[ident.Name]
	case *ast.TypeAssertExpr:
		return readsTrackedMap(e.X, tracked)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			if readsTrackedMap(arg, tracked) {
				return true
			}
		}
	}
	return false
}

// isConfigMapType matches the shapes an operator's configuration arrives
// in: map[string]interface{}, map[string]any, and the named alias the
// strategy factory passes.
func isConfigMapType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.MapType:
		key, ok := t.Key.(*ast.Ident)
		if !ok || key.Name != "string" {
			return false
		}
		switch v := t.Value.(type) {
		case *ast.InterfaceType:
			return v.Methods == nil || len(v.Methods.List) == 0
		case *ast.Ident:
			return v.Name == "any"
		}
	case *ast.SelectorExpr:
		return t.Sel.Name == "StorageConfigParams" || t.Sel.Name == "ProbeParams"
	case *ast.Ident:
		return t.Name == "StorageConfigParams" || t.Name == "ProbeParams"
	}
	return false
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func documentationCorpus(t *testing.T, root string) string {
	t.Helper()

	var b strings.Builder
	dir := filepath.Join(root, docRoot)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		b.Write(content)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s is empty — the check would pass by finding nothing", dir)
	}
	return b.String()
}

// moduleRoot walks up to the directory holding go.mod, so the test does
// not care where it is run from.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the working directory")
		}
		dir = parent
	}
}
