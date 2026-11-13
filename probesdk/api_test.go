package probesdk_test

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const (
	modulePrefix          = "senhub-agent.go/"
	appPackage            = "senhub-agent.go/app"
	apiChangedInstruction = "probesdk API changed. If intended, run: UPDATE_GOLDEN=1 go test ./probesdk/ -run TestAPISnapshot -count=1 (or: make probesdk-api) and commit probesdk/api.txt; the enterprise repository builds against this surface."
)

// The enterprise repository imports these entry points from the app package
// directly, so they belong to the surface even though they live outside
// probesdk. The rest of app is not part of the contract.
var appEntryPoints = map[string]bool{
	"Main":            true,
	"RegisterCommand": true,
	"ExtraCommand":    true,
}

type snapshotBuilder struct {
	imports     map[string]string
	imp         types.Importer
	rendered    map[types.Type]bool
	reachQueued map[types.Type]bool
	reachQueue  []*types.Named
}

func TestAPISnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The importer drops packages whose export data comes from a
		// cross-compiled build on the Windows runner; the surface is one
		// file for every platform and the Linux job checks it.
		t.Skip("the API snapshot is checked on Linux")
	}
	got, err := renderSnapshot()
	if err != nil {
		t.Fatalf("render API snapshot: %v", err)
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile("api.txt", []byte(got), 0o644); err != nil {
			t.Fatalf("write api.txt: %v", err)
		}
		t.Logf("wrote api.txt")
		return
	}

	want, err := os.ReadFile("api.txt")
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("api.txt missing:\n%s\n%s", lineDiff("", got), apiChangedInstruction)
		}
		t.Fatalf("read api.txt: %v", err)
	}

	if got != string(want) {
		t.Fatalf("probesdk API snapshot mismatch:\n%s\n%s", lineDiff(string(want), got), apiChangedInstruction)
	}
}

func TestAPISnapshotIsDeterministic(t *testing.T) {
	first, err := renderSnapshot()
	if err != nil {
		t.Fatalf("render first API snapshot: %v", err)
	}
	second, err := renderSnapshot()
	if err != nil {
		t.Fatalf("render second API snapshot: %v", err)
	}
	if first != second {
		t.Fatalf("probesdk API snapshot is not deterministic:\n%s", lineDiff(first, second))
	}
}

func renderSnapshot() (string, error) {
	root, err := findModuleRoot()
	if err != nil {
		return "", err
	}

	exports, err := loadExportMap(root)
	if err != nil {
		return "", err
	}

	pkgPaths, err := loadPackageList(root)
	if err != nil {
		return "", err
	}

	b := &snapshotBuilder{
		imports:     exports,
		rendered:    make(map[types.Type]bool),
		reachQueued: make(map[types.Type]bool),
	}
	b.imp = importer.ForCompiler(token.NewFileSet(), "gc", b.lookup)

	var out strings.Builder
	for _, pkgPath := range pkgPaths {
		// A test-only directory (for example the root probesdk package
		// itself, which contains only this file) has no export data and
		// therefore no API snapshot entry.
		if _, ok := b.imports[pkgPath]; !ok {
			continue
		}

		pkg, err := b.imp.Import(pkgPath)
		if err != nil {
			return "", fmt.Errorf("import %q: %w", pkgPath, err)
		}

		b.renderPackage(&out, pkg)
	}

	b.collectAllReachable()
	b.renderReachable(&out)

	return out.String(), nil
}

func findModuleRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat go.mod in %s: %w", wd, err)
		}

		parent := filepath.Dir(wd)
		if parent == wd {
			return "", fmt.Errorf("go.mod not found from %s", wd)
		}
		wd = parent
	}
}

func loadExportMap(root string) (map[string]string, error) {
	cmd := exec.Command("go", "list", "-export", "-deps", "-f", "{{.ImportPath}}={{.Export}}", "./probesdk/...", "./app")
	cmd.Dir = root
	cmd.Env = snapshotEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go list -export -deps ./probesdk/...: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	exports := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		importPath := strings.TrimSpace(parts[0])
		exportFile := strings.TrimSpace(parts[1])
		if importPath == "" || exportFile == "" {
			continue
		}

		exports[importPath] = exportFile
	}

	return exports, nil
}

func loadPackageList(root string) ([]string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}", "./probesdk/...")
	cmd.Dir = root
	cmd.Env = snapshotEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go list ./probesdk/...: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	var pkgs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			pkgs = append(pkgs, line)
		}
	}

	pkgs = append(pkgs, appPackage)
	sort.Strings(pkgs)
	return pkgs, nil
}

func (b *snapshotBuilder) lookup(path string) (io.ReadCloser, error) {
	exportFile, ok := b.imports[path]
	if !ok {
		return nil, fmt.Errorf("missing export data for %q", path)
	}

	r, err := os.Open(exportFile)
	if err != nil {
		return nil, fmt.Errorf("open export data for %q (%s): %w", path, exportFile, err)
	}
	return r, nil
}

func (b *snapshotBuilder) renderPackage(buf *strings.Builder, pkg *types.Package) {
	fmt.Fprintf(buf, "== %s (package %s) ==\n", pkg.Path(), pkg.Name())

	names := pkg.Scope().Names()
	sort.Strings(names)

	for _, name := range names {
		obj := pkg.Scope().Lookup(name)
		if obj == nil || !obj.Exported() {
			continue
		}
		if pkg.Path() == appPackage && !appEntryPoints[name] {
			continue
		}
		b.renderObject(buf, obj)
	}
}

func (b *snapshotBuilder) renderObject(buf *strings.Builder, obj types.Object) {
	switch obj := obj.(type) {
	case *types.Const:
		fmt.Fprintf(buf, "const %s %s = %s\n", obj.Name(), b.typeString(obj.Type()), obj.Val().ExactString())
		b.collectType(obj.Type())

	case *types.Var:
		fmt.Fprintf(buf, "var %s %s\n", obj.Name(), b.typeString(obj.Type()))
		b.collectType(obj.Type())

	case *types.Func:
		sig, ok := obj.Type().(*types.Signature)
		if !ok {
			return
		}
		fmt.Fprintf(buf, "func %s%s\n", obj.Name(), b.signatureAfterName(sig))
		b.collectSignature(sig)

	case *types.TypeName:
		b.renderTypeName(buf, obj)
	}
}

func (b *snapshotBuilder) renderTypeName(buf *strings.Builder, obj *types.TypeName) {
	target := types.Unalias(obj.Type())

	if obj.IsAlias() {
		fmt.Fprintf(buf, "alias %s = %s\n", obj.Name(), b.typeString(target))

		named, ok := target.(*types.Named)
		if ok {
			b.markRendered(named)
			b.expandNamed(buf, named, "  ")
		} else {
			fmt.Fprintf(buf, "  underlying %s\n", b.typeString(target))
			b.collectType(target)
		}
		return
	}

	named, ok := target.(*types.Named)
	if !ok {
		fmt.Fprintf(buf, "type %s %s\n", obj.Name(), b.typeString(target))
		b.collectType(target)
		return
	}

	fmt.Fprintf(buf, "type %s %s\n", obj.Name(), b.typeKind(named))
	b.markRendered(named)
	b.expandNamed(buf, named, "  ")
}

func (b *snapshotBuilder) expandNamed(buf *strings.Builder, named *types.Named, indent string) {
	b.collectTypeArgs(named)

	switch u := named.Underlying().(type) {
	case *types.Struct:
		b.renderStruct(buf, u, indent)
	case *types.Interface:
		b.renderInterface(buf, u, indent)
	default:
		fmt.Fprintf(buf, "%sunderlying %s\n", indent, b.typeString(named.Underlying()))
		b.collectType(named.Underlying())
	}

	b.renderPointerMethods(buf, named, indent)
}

type structFieldInfo struct {
	name     string
	typeStr  string
	tag      string
	sortKey  string
	embedded bool
	typ      types.Type
}

func (b *snapshotBuilder) renderStruct(buf *strings.Builder, st *types.Struct, indent string) {
	var fields []structFieldInfo

	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		if !field.Exported() {
			continue
		}

		if field.Embedded() {
			typeStr := b.typeString(field.Type())
			fields = append(fields, structFieldInfo{
				typeStr:  typeStr,
				sortKey:  "embedded:" + typeStr,
				embedded: true,
				typ:      field.Type(),
			})
			continue
		}

		typeStr := b.typeString(field.Type())
		fields = append(fields, structFieldInfo{
			name:    field.Name(),
			typeStr: typeStr,
			tag:     st.Tag(i),
			sortKey: "field:" + field.Name(),
			typ:     field.Type(),
		})
	}

	sort.Slice(fields, func(i, j int) bool {
		return fields[i].sortKey < fields[j].sortKey
	})

	for _, field := range fields {
		if field.embedded {
			fmt.Fprintf(buf, "%sembedded %s\n", indent, field.typeStr)
		} else if field.tag != "" {
			fmt.Fprintf(buf, "%sfield %s %s `%s`\n", indent, field.name, field.typeStr, field.tag)
		} else {
			fmt.Fprintf(buf, "%sfield %s %s\n", indent, field.name, field.typeStr)
		}
		b.collectType(field.typ)
	}
}

type interfaceMethodInfo struct {
	name    string
	sig     string
	sortKey string
	typ     *types.Signature
}

type interfaceEmbeddedInfo struct {
	typeStr string
	typ     types.Type
}

func (b *snapshotBuilder) renderInterface(buf *strings.Builder, iface *types.Interface, indent string) {
	var methods []interfaceMethodInfo
	for i := 0; i < iface.NumMethods(); i++ {
		method := iface.Method(i)
		sig, ok := method.Type().(*types.Signature)
		if !ok {
			continue
		}
		methods = append(methods, interfaceMethodInfo{
			name:    method.Name(),
			sig:     b.signatureAfterName(sig),
			sortKey: "method:" + method.Name(),
			typ:     sig,
		})
	}

	sort.Slice(methods, func(i, j int) bool {
		return methods[i].sortKey < methods[j].sortKey
	})

	for _, method := range methods {
		fmt.Fprintf(buf, "%simethod %s%s\n", indent, method.name, method.sig)
		b.collectSignature(method.typ)
	}

	var embedded []interfaceEmbeddedInfo
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		typ := iface.EmbeddedType(i)
		embedded = append(embedded, interfaceEmbeddedInfo{
			typeStr: b.typeString(typ),
			typ:     typ,
		})
	}

	sort.Slice(embedded, func(i, j int) bool {
		return embedded[i].typeStr < embedded[j].typeStr
	})

	for _, embeddedInfo := range embedded {
		fmt.Fprintf(buf, "%sembedded %s\n", indent, embeddedInfo.typeStr)
		b.collectType(embeddedInfo.typ)
	}
}

func (b *snapshotBuilder) renderPointerMethods(buf *strings.Builder, named *types.Named, indent string) {
	ms := types.NewMethodSet(types.NewPointer(named))

	var selections []*types.Selection
	for i := 0; i < ms.Len(); i++ {
		sel := ms.At(i)
		if sel.Obj().Exported() {
			selections = append(selections, sel)
		}
	}

	sort.Slice(selections, func(i, j int) bool {
		return selections[i].Obj().Name() < selections[j].Obj().Name()
	})

	for _, sel := range selections {
		fn, ok := sel.Obj().(*types.Func)
		if !ok {
			continue
		}

		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}

		recv := sig.Recv()
		if recv != nil {
			if ptr, ok := recv.Type().(*types.Pointer); ok {
				fmt.Fprintf(buf, "%smethod (*%s) %s%s\n", indent, b.typeString(ptr.Elem()), fn.Name(), b.signatureAfterName(sig))
				b.collectSignature(sig)
				continue
			}
		}

		fmt.Fprintf(buf, "%smethod %s%s\n", indent, fn.Name(), b.signatureAfterName(sig))
		b.collectSignature(sig)
	}
}

func (b *snapshotBuilder) collectAllReachable() {
	// Seed collection from every SDK-rendered type. Iterating over a map is
	// fine here because the final reachable section is sorted later.
	for t := range b.rendered {
		if named, ok := t.(*types.Named); ok {
			b.collectReachableFromNamed(named)
		}
	}

	for i := 0; i < len(b.reachQueue); i++ {
		b.collectReachableFromNamed(b.reachQueue[i])
	}
}

func (b *snapshotBuilder) collectReachableFromNamed(named *types.Named) {
	b.collectTypeArgs(named)

	switch u := named.Underlying().(type) {
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			field := u.Field(i)
			if field.Exported() {
				b.collectType(field.Type())
			}
		}
	case *types.Interface:
		for i := 0; i < u.NumMethods(); i++ {
			if sig, ok := u.Method(i).Type().(*types.Signature); ok {
				b.collectSignature(sig)
			}
		}
		for i := 0; i < u.NumEmbeddeds(); i++ {
			b.collectType(u.EmbeddedType(i))
		}
	default:
		b.collectType(named.Underlying())
	}

	ms := types.NewMethodSet(types.NewPointer(named))
	for i := 0; i < ms.Len(); i++ {
		sel := ms.At(i)
		if !sel.Obj().Exported() {
			continue
		}
		if sig, ok := sel.Obj().Type().(*types.Signature); ok {
			b.collectSignature(sig)
		}
	}
}

func (b *snapshotBuilder) renderReachable(buf *strings.Builder) {
	if len(b.reachQueue) == 0 {
		return
	}

	queue := append([]*types.Named(nil), b.reachQueue...)
	sort.Slice(queue, func(i, j int) bool {
		left := namedFullPath(queue[i])
		right := namedFullPath(queue[j])
		if left != right {
			return left < right
		}
		return b.typeString(queue[i]) < b.typeString(queue[j])
	})

	fmt.Fprintln(buf, "== reachable types ==")

	for _, named := range queue {
		b.markRendered(named)
		fmt.Fprintf(buf, "type %s %s\n", b.typeString(named), b.typeKind(named))
		b.expandNamed(buf, named, "  ")
	}
}

func (b *snapshotBuilder) collectType(t types.Type) {
	if t == nil {
		return
	}

	t = types.Unalias(t)

	switch u := t.(type) {
	case *types.Named:
		if pkg := u.Obj().Pkg(); pkg != nil && strings.HasPrefix(pkg.Path(), modulePrefix) && !b.rendered[u] {
			b.enqueue(u)
		}
		b.collectTypeArgs(u)

	case *types.Pointer:
		b.collectType(u.Elem())

	case *types.Slice:
		b.collectType(u.Elem())

	case *types.Array:
		b.collectType(u.Elem())

	case *types.Map:
		b.collectType(u.Key())
		b.collectType(u.Elem())

	case *types.Chan:
		b.collectType(u.Elem())

	case *types.Signature:
		b.collectSignature(u)

	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			b.collectType(u.Field(i).Type())
		}

	case *types.Interface:
		for i := 0; i < u.NumMethods(); i++ {
			if sig, ok := u.Method(i).Type().(*types.Signature); ok {
				b.collectSignature(sig)
			}
		}
		for i := 0; i < u.NumEmbeddeds(); i++ {
			b.collectType(u.EmbeddedType(i))
		}

	case *types.Tuple:
		for i := 0; i < u.Len(); i++ {
			b.collectType(u.At(i).Type())
		}

	case *types.Union:
		for i := 0; i < u.Len(); i++ {
			b.collectType(u.Term(i).Type())
		}

	case *types.TypeParam:
		b.collectType(u.Constraint())
	}
}

func (b *snapshotBuilder) collectSignature(sig *types.Signature) {
	if sig == nil {
		return
	}

	if recv := sig.Recv(); recv != nil {
		b.collectType(recv.Type())
	}
	if params := sig.Params(); params != nil {
		b.collectTuple(params)
	}
	if results := sig.Results(); results != nil {
		b.collectTuple(results)
	}
	if tparams := sig.TypeParams(); tparams != nil {
		for i := 0; i < tparams.Len(); i++ {
			b.collectType(tparams.At(i).Constraint())
		}
	}
	if recvTypeParams := sig.RecvTypeParams(); recvTypeParams != nil {
		for i := 0; i < recvTypeParams.Len(); i++ {
			b.collectType(recvTypeParams.At(i).Constraint())
		}
	}
}

func (b *snapshotBuilder) collectTuple(tuple *types.Tuple) {
	if tuple == nil {
		return
	}
	for i := 0; i < tuple.Len(); i++ {
		b.collectType(tuple.At(i).Type())
	}
}

func (b *snapshotBuilder) collectTypeArgs(named *types.Named) {
	if args := named.TypeArgs(); args != nil {
		for i := 0; i < args.Len(); i++ {
			b.collectType(args.At(i))
		}
	}
	if tparams := named.TypeParams(); tparams != nil {
		for i := 0; i < tparams.Len(); i++ {
			b.collectType(tparams.At(i).Constraint())
		}
	}
}

func (b *snapshotBuilder) enqueue(named *types.Named) {
	if b.rendered[named] || b.reachQueued[named] {
		return
	}
	b.reachQueued[named] = true
	b.reachQueue = append(b.reachQueue, named)
}

func (b *snapshotBuilder) markRendered(named *types.Named) {
	b.rendered[named] = true
}

func (b *snapshotBuilder) typeString(t types.Type) string {
	return types.TypeString(t, func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		return pkg.Path()
	})
}

func (b *snapshotBuilder) typeKind(named *types.Named) string {
	switch named.Underlying().(type) {
	case *types.Struct:
		return "struct"
	case *types.Interface:
		return "interface"
	default:
		return b.typeString(named.Underlying())
	}
}

func (b *snapshotBuilder) signatureAfterName(sig *types.Signature) string {
	s := b.typeString(sig)
	return strings.TrimSpace(strings.TrimPrefix(s, "func"))
}

func namedFullPath(named *types.Named) string {
	obj := named.Obj()
	if pkg := obj.Pkg(); pkg != nil {
		return pkg.Path() + "." + obj.Name()
	}
	return obj.Name()
}

func lineDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	ops := diffOps(wantLines, gotLines)
	if len(ops) > 80 {
		ops = ops[:80]
	}

	return strings.Join(ops, "\n")
}

func diffOps(a, b []string) []string {
	n := len(a)
	m := len(b)

	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		ops := make([]string, 0, m)
		for _, line := range b {
			ops = append(ops, "+"+line)
		}
		return ops
	}
	if m == 0 {
		ops := make([]string, 0, n)
		for _, line := range a {
			ops = append(ops, "-"+line)
		}
		return ops
	}

	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []string
	i, j := 0, 0

	for i < n && j < m {
		if a[i] == b[j] {
			i++
			j++
			continue
		}

		if lcs[i+1][j] >= lcs[i][j+1] {
			ops = append(ops, "-"+a[i])
			i++
		} else {
			ops = append(ops, "+"+b[j])
			j++
		}
	}

	for ; i < n; i++ {
		ops = append(ops, "-"+a[i])
	}
	for ; j < m; j++ {
		ops = append(ops, "+"+b[j])
	}

	return ops
}

// snapshotEnv renders the surface for linux/amd64 whatever the host: files
// behind build constraints change it, and api.txt is one file for every
// runner.
func snapshotEnv() []string {
	return append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
}
