package probes_test

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

	"senhub-agent.go/internal/agent/probes"
)

// TestProbeSpecs_MatchWhatParsersRead keeps every declared schema honest
// against the parser it describes, in both directions: a key the parser
// reads but the spec does not declare would be reported as unknown by the
// configurator on a working file; a key the spec declares but nothing
// reads would offer the operator a setting with no effect.
//
// Keys are found by static analysis of the probe's package (and the probe
// packages it imports, such as hostpoll), following the operator's params
// map the way internal/docscoverage does.
func TestProbeSpecs_MatchWhatParsersRead(t *testing.T) {
	root := moduleRoot(t)
	dirs := probePackageDirs(t, root)

	for _, spec := range probes.RegisteredProbeSpecs() {
		dir, ok := dirs[spec.Type]
		if !ok {
			t.Errorf("spec %q: no package under internal/agent/probes registers this type", spec.Type)
			continue
		}
		read := keysReadByPackage(t, root, dir)
		declared := declaredSegments(spec)

		var undeclared, unread []string
		for key := range read {
			if _, ok := declared[key]; !ok && !guardAllowed(spec.Type, key) {
				undeclared = append(undeclared, key)
			}
		}
		for key := range declared {
			if _, ok := read[key]; !ok && !guardAllowed(spec.Type, key) {
				unread = append(unread, key)
			}
		}
		sort.Strings(undeclared)
		sort.Strings(unread)
		if len(undeclared) > 0 {
			t.Errorf("spec %q: the parser reads %v but the spec does not declare them", spec.Type, undeclared)
		}
		if len(unread) > 0 {
			t.Errorf("spec %q: declares %v but nothing in %s reads them", spec.Type, unread, dir)
		}
	}
}

// guardAllowed lists keys a package reads that are not operator settings,
// or settings read somewhere the scan cannot follow. Every entry needs a
// reason; a stale entry should be removed when the reason goes away.
var guardAllowList = map[string]map[string]string{
	"syslog": {
		// Fields of the parsed syslog message handed to processLogMessage,
		// not settings; "host" is the scan seeing Str("host", hostname).
		"app_name": "parsed message field", "client": "parsed message field", "content": "parsed message field",
		"facility": "parsed message field", "hostname": "parsed message field", "message": "parsed message field",
		"priority": "parsed message field", "severity": "parsed message field", "tag": "parsed message field",
		"timestamp": "parsed message field", "host": "log field name, not a setting",
	},
	"event": {
		// Fields of the decoded JSON body of a posted event, not settings.
		"host": "event body field", "message": "event body field", "severity": "event body field", "timestamp": "event body field",
	},
	"solr": {
		"count":   "field of the decoded /admin/metrics response, not a setting",
		"hits":    "field of the decoded /admin/metrics response, not a setting",
		"inserts": "field of the decoded /admin/metrics response, not a setting",
	},
}

func guardAllowed(probeType, key string) bool {
	_, ok := guardAllowList[probeType][key]
	return ok
}

// declaredSegments flattens a spec to bare key names at every depth, the
// same shape the static scan produces (it records literal keys, not
// dotted paths).
func declaredSegments(spec probes.ProbeSpec) map[string]struct{} {
	out := map[string]struct{}{}
	for path := range spec.DeclaredKeys() {
		for _, seg := range strings.Split(path, ".") {
			out[seg] = struct{}{}
		}
	}
	return out
}

// probePackageDirs maps a probe type to the package directory whose init
// registers it, by reading the RegisterProbe("...") calls.
func probePackageDirs(t *testing.T, root string) map[string]string {
	t.Helper()
	// A probe registers itself with a literal or with a package constant;
	// the spec always carries the literal, so both are read.
	re := regexp.MustCompile(`RegisterProbe\(\s*"([a-z0-9_]+)"|\bType:\s*"([a-z0-9_]+)"`)
	out := map[string]string{}
	base := filepath.Join(root, "internal", "agent", "probes")
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, readErr := os.ReadFile(path) // #nosec G304 - the repository's own source
		if readErr != nil {
			return readErr
		}
		// Only a probe's own package registers it; a mention in the
		// registry package's docs or examples is not a registration.
		if filepath.Dir(path) == base {
			return nil
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			out[name] = filepath.Dir(path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", base, err)
	}
	return out
}

// keysReadByPackage collects the params keys read in dir and in the probe
// packages it imports (one level), so a probe that delegates its parsing
// to a shared package such as hostpoll is still covered.
func keysReadByPackage(t *testing.T, root, dir string) map[string]struct{} {
	t.Helper()
	fset := token.NewFileSet()
	// Follow the imports a parser delegates to, one level: sibling probe
	// packages (hostpoll for the host probes) and the governance package
	// (snmp_poll). Wider first-party imports are not followed: entity
	// helpers take attribute maps of the same Go type as a params map,
	// and their keys are not settings.
	const modulePrefix = "senhub-agent.go/"
	followed := []string{modulePrefix + "internal/agent/probes/", modulePrefix + "internal/agent/services/governance"}
	// dbcommon builds entity attribute maps (typed like a params map) for
	// the database probes; its keys are OTel attributes, not settings.
	skipped := []string{modulePrefix + "internal/agent/probes/dbcommon"}
	dirs := []string{dir}
	if files, err := parseDirectory(fset, dir); err == nil {
		for _, f := range files {
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				skip := false
				for _, prefix := range skipped {
					if strings.HasPrefix(p, prefix) {
						skip = true
					}
				}
				if skip {
					continue
				}
				for _, prefix := range followed {
					if strings.HasPrefix(p, prefix) {
						dirs = append(dirs, filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, modulePrefix))))
					}
				}
			}
		}
	}
	keys := map[string]struct{}{}
	for _, d := range dirs {
		parsed, err := parseDirectory(fset, d)
		if err != nil {
			t.Fatalf("parsing %s: %v", d, err)
		}
		returners := configMapReturners(parsed)
		for _, file := range parsed {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				for key := range keysInFunc(fn, returners) {
					keys[key] = struct{}{}
				}
			}
		}
	}
	return keys
}

var configKeyRe = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

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
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
			return true
		}
		switch rhs := assign.Rhs[0].(type) {
		case *ast.CallExpr:
			callee, ok := rhs.Fun.(*ast.Ident)
			if !ok || !mapReturners[callee.Name] {
				return true
			}
		case *ast.TypeAssertExpr:
			// `block, ok := raw.(map[string]interface{})` on a value that
			// arrived as interface{} (a nested block handed to a helper, or
			// an element of a list of blocks): the result is a settings
			// map whatever it came from.
			if rhs.Type == nil || !isConfigMapType(rhs.Type) {
				return true
			}
		default:
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

// record keeps snake_case keys of two characters or more: "v3" is a real
// block in snmp_poll, which is why this is looser than docscoverage.
func record(found map[string]token.Pos, key string, pos token.Pos) {
	if !configKeyRe.MatchString(key) || len(key) < 2 {
		return
	}
	if _, seen := found[key]; !seen {
		found[key] = pos
	}
}

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
		return t.Sel.Name == "StorageConfigParams" || t.Sel.Name == "ProbeParams" || t.Sel.Name == "ProbeConfigParams"
	case *ast.Ident:
		return t.Name == "StorageConfigParams" || t.Name == "ProbeParams" || t.Name == "ProbeConfigParams"
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
