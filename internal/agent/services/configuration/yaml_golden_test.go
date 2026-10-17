package configuration

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

// This file pins what the YAML layer reads and writes: every example,
// packaging and documentation configuration of the repository plus a corpus
// of awkward documents is loaded and dumped with its Go types, and the files
// the agent writes are compared byte for byte. A change of YAML library must
// leave every golden file untouched. UPDATE_YAML_GOLDEN=1 rewrites them.

const repoRoot = "../../../.."

var (
	timestampPattern = regexp.MustCompile(`\d{8}-\d{6}`)
	fenceStart       = regexp.MustCompile("(?m)^```ya?ml[^\\n]*\\n")
)

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("testdata", "yaml-golden", name)
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
	if string(want) == got {
		return
	}
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

// dumpTyped renders a value with its Go types, maps sorted by key, so that
// map[string]interface{} and map[interface{}]interface{} stay distinguishable.
func dumpTyped(b *strings.Builder, v reflect.Value, depth int) {
	pad := strings.Repeat("  ", depth)
	if !v.IsValid() {
		b.WriteString("nil\n")
		return
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			fmt.Fprintf(b, "nil(%s)\n", v.Type())
			return
		}
		if v.Kind() == reflect.Ptr {
			b.WriteString("&")
		}
		dumpTyped(b, v.Elem(), depth)
	case reflect.Struct:
		fmt.Fprintf(b, "%s{\n", v.Type())
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			fmt.Fprintf(b, "%s  %s: ", pad, v.Type().Field(i).Name)
			dumpTyped(b, v.Field(i), depth+1)
		}
		fmt.Fprintf(b, "%s}\n", pad)
	case reflect.Map:
		if v.IsNil() {
			fmt.Fprintf(b, "nil(%s)\n", v.Type())
			return
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool {
			return fmt.Sprintf("%T:%v", keys[i].Interface(), keys[i].Interface()) < fmt.Sprintf("%T:%v", keys[j].Interface(), keys[j].Interface())
		})
		fmt.Fprintf(b, "%s{\n", v.Type())
		for _, k := range keys {
			fmt.Fprintf(b, "%s  ", pad)
			dumpTyped(b, k, depth+1)
			fmt.Fprintf(b, "%s  =>", pad)
			dumpTyped(b, v.MapIndex(k), depth+1)
		}
		fmt.Fprintf(b, "%s}\n", pad)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			fmt.Fprintf(b, "nil(%s)\n", v.Type())
			return
		}
		fmt.Fprintf(b, "%s[\n", v.Type())
		for i := 0; i < v.Len(); i++ {
			fmt.Fprintf(b, "%s  ", pad)
			dumpTyped(b, v.Index(i), depth+1)
		}
		fmt.Fprintf(b, "%s]\n", pad)
	case reflect.String:
		fmt.Fprintf(b, "%s(%q)\n", v.Type(), v.String())
	default:
		fmt.Fprintf(b, "%s(%v)\n", v.Type(), v.Interface())
	}
}

func typedDump(v interface{}) string {
	var b strings.Builder
	dumpTyped(&b, reflect.ValueOf(v), 0)
	return b.String()
}

func scrub(dir, s string) string {
	s = strings.ReplaceAll(s, dir, "<DIR>")
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		s = strings.ReplaceAll(s, resolved, "<DIR>")
	}
	return timestampPattern.ReplaceAllString(s, "<TS>")
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readTree(t *testing.T, dir string) string {
	t.Helper()
	var paths []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(&b, "===== %s =====\n%s", scrub(dir, rel), scrub(dir, string(body)))
		if !bytes.HasSuffix(body, []byte("\n")) {
			b.WriteString("\n<no trailing newline>\n")
		}
	}
	return b.String()
}

type corpusCase struct {
	name string
	// stage lays the case out in dir and returns the path of agent.yaml.
	stage func(t *testing.T, dir string) string
	// monolithic is true when the case is a single legacy file.
	monolithic bool
}

func singleFileCase(name, src string) corpusCase {
	return corpusCase{name: name, monolithic: true, stage: func(t *testing.T, dir string) string {
		body, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "agent.yaml")
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}}
}

func treeCase(name, src string) corpusCase {
	return corpusCase{name: name, stage: func(t *testing.T, dir string) string {
		copyTree(t, src, dir)
		return filepath.Join(dir, "agent.yaml")
	}}
}

func repoCorpus(t *testing.T) []corpusCase {
	t.Helper()
	var cases []corpusCase
	for _, pattern := range []string{
		filepath.Join(repoRoot, "examples", "*.yaml"),
		filepath.Join(repoRoot, "docs", "probes", "*", "configuration-examples.yaml"),
		filepath.Join("testdata", "yaml-corpus", "*.yaml"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(matches)
		for _, m := range matches {
			rel := filepath.ToSlash(m)
			rel = strings.TrimPrefix(rel, "../../../../")
			cases = append(cases, singleFileCase(rel, m))
		}
	}
	for _, d := range []string{
		filepath.Join(repoRoot, "examples", "example-config-multi-file"),
		filepath.Join(repoRoot, "packaging", "nfpm", "conf"),
		filepath.Join("testdata", "yaml-corpus", "tricky-multi"),
	} {
		cases = append(cases, treeCase(strings.TrimPrefix(filepath.ToSlash(d), "../../../../"), d))
	}
	return cases
}

// docSnippetCases extracts, from the documentation, the YAML blocks that are
// a whole configuration or a list of probes.
func docSnippetCases(t *testing.T) []corpusCase {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(filepath.Join(repoRoot, "docs"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	var cases []corpusCase
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		rel := strings.TrimPrefix(filepath.ToSlash(f), "../../../../")
		for i, loc := range fenceStart.FindAllStringIndex(text, -1) {
			rest := text[loc[1]:]
			end := strings.Index(rest, "\n```")
			if end < 0 {
				continue
			}
			body := rest[:end+1]
			name := fmt.Sprintf("%s#%d", rel, i)
			switch {
			case regexp.MustCompile(`(?m)^(probes|storage):`).MatchString(body):
				cases = append(cases, corpusCase{name: name, monolithic: true, stage: func(t *testing.T, dir string) string {
					p := filepath.Join(dir, "agent.yaml")
					if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
					return p
				}})
			case strings.HasPrefix(body, "- name:") && strings.Contains(body, "type:"):
				cases = append(cases, corpusCase{name: name, stage: func(t *testing.T, dir string) string {
					p := filepath.Join(dir, "agent.yaml")
					if err := os.WriteFile(p, []byte("config_version: 3\nagent:\n  key: k\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-snippet.yaml"), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
					return p
				}})
			}
		}
	}
	return cases
}

// stubSecrets answers every ${secret:} reference, so a documentation example
// that names a secret still loads and its whole content is pinned.
type stubSecrets struct{}

func (stubSecrets) Get(name string) (string, error) { return "stub:" + name, nil }
func (stubSecrets) Set(string, secret.Secret) error { return nil }
func (stubSecrets) Delete(string) error             { return nil }
func (stubSecrets) Name() string                    { return "stub" }
func (stubSecrets) List() ([]string, error)         { return nil, nil }

func useStubSecrets(t *testing.T) {
	t.Helper()
	secret.SetProvider(stubSecrets{})
	t.Cleanup(func() { secret.SetProvider(nil) })
}

func loadDump(t *testing.T, c corpusCase) string {
	t.Helper()
	dir := t.TempDir()
	path := c.stage(t, dir)
	data, err := LoadFromDisk(path, nil)
	if err != nil {
		return "ERROR: " + scrub(dir, err.Error()) + "\n"
	}
	return scrub(dir, typedDump(data))
}

func TestYAMLGolden_LoadCorpus(t *testing.T) {
	useStubSecrets(t)
	var b strings.Builder
	for _, c := range repoCorpus(t) {
		fmt.Fprintf(&b, "##### %s\n%s", c.name, loadDump(t, c))
	}
	assertGolden(t, "load-corpus.golden", b.String())
}

func TestYAMLGolden_LoadDocSnippets(t *testing.T) {
	useStubSecrets(t)
	cases := docSnippetCases(t)
	if len(cases) < 20 {
		t.Fatalf("only %d documentation snippets found; the extraction is broken", len(cases))
	}
	var b strings.Builder
	for _, c := range cases {
		out := loadDump(t, c)
		status := "ok"
		if strings.HasPrefix(out, "ERROR:") {
			status = "error"
		}
		fmt.Fprintf(&b, "%s %s %x\n", c.name, status, sha256.Sum256([]byte(out)))
	}
	assertGolden(t, "load-doc-snippets.golden", b.String())
}

func TestYAMLGolden_FragmentReaders(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, filepath.Join("testdata", "yaml-corpus", "tricky-multi"), dir)
	path := filepath.Join(dir, "agent.yaml")
	var b strings.Builder

	frags, err := ListStrategyFragments(path)
	fmt.Fprintf(&b, "ListStrategyFragments err=%v\n%s", err, scrub(dir, typedDump(frags)))
	for _, name := range []string{"http", "otlp"} {
		params, err := StrategyFragmentParams(path, name)
		fmt.Fprintf(&b, "StrategyFragmentParams(%s) err=%v\n%s", name, err, typedDump(params))
	}
	for _, name := range []string{"web", "other", "dup", "absent"} {
		p, ok, err := ReadProbeFragment(path, name)
		fmt.Fprintf(&b, "ReadProbeFragment(%s) ok=%v err=%v\n%s", name, ok, err, typedDump(p))
		params, err := ReadProbeFragmentParams(path, name)
		fmt.Fprintf(&b, "ReadProbeFragmentParams(%s) err=%v\n%s", name, err, typedDump(params))
	}
	types, err := probeTypesConfigured(filepath.Join(dir, "probes.d"))
	fmt.Fprintf(&b, "probeTypesConfigured err=%v\n%s", err, typedDump(types))

	for _, c := range repoCorpus(t) {
		d := t.TempDir()
		p := c.stage(t, d)
		raw, _ := os.ReadFile(p)
		fmt.Fprintf(&b, "HasMonolithicMarkers(%s)=%v\n", c.name, HasMonolithicMarkers(raw))
	}
	assertGolden(t, "fragment-readers.golden", scrub(dir, b.String()))
}

func richProbe() ProbeConfig {
	on := true
	return ProbeConfig{
		Name: "rich",
		Type: "demo",
		Params: map[string]interface{}{
			"host":     "db.example.com",
			"port":     5432,
			"ratio":    1.5,
			"whole":    2.0,
			"flag":     true,
			"words":    []interface{}{"yes", "no", "on", "off", "y", "n", "true", "null", "~", "1e3", "0x10", "010", "2024-01-02", "", " lead", "trail ", "a: b", "# c", "- d", "@at", "`tick", "%pct", "!bang", "&amp", "*star", "|pipe", ">gt", "'sq", "\"dq", "tab\there", "uni é 日本", "{br}", "[sq]", "x,y"},
			"multi":    "line one\nline two\n",
			"multi2":   "no trailing newline\nsecond",
			"long":     strings.Repeat("word ", 60),
			"longword": strings.Repeat("x", 200),
			"nested": map[string]interface{}{
				"list":  []interface{}{"a", "b", map[string]interface{}{"k": "v", "n": []interface{}{1, 2, 3}}},
				"empty": map[string]interface{}{},
				"none":  []interface{}{},
				"deep":  map[string]interface{}{"deeper": map[string]interface{}{"deepest": []interface{}{[]interface{}{1, 2}, []interface{}{3}}}},
			},
			"ref":    "${env:SOME_VAR:-default}",
			"dollar": "price $5",
			"yesstr": "yes",
		},
		CustomTags:    map[string]string{"env": "prod", "yes": "no", "num": "5", "empty": ""},
		Governance:    map[string]interface{}{"criticality": "high", "owner": map[string]interface{}{"team": "sre", "contacts": []interface{}{"a@x", "b@x"}}},
		Enabled:       &on,
		LogStrategies: []string{"otlp", "event"},
	}
}

func richStrategy() StorageConfigParams {
	return StorageConfigParams{
		"port":      8080,
		"bind":      "0.0.0.0",
		"endpoints": []interface{}{"prtg", "web"},
		"tls":       map[string]interface{}{"enabled": true, "cipher_suites": []interface{}{"A", "B"}, "min": "1.2"},
		"ratio":     0.5,
		"word":      "yes",
		"long":      strings.Repeat("segment ", 40),
		"multi":     "a\nb\n",
	}
}

func TestYAMLGolden_FragmentWriters(t *testing.T) {
	secret.SetProvider(secret.NewMemoryProvider())
	t.Cleanup(func() { secret.SetProvider(nil) })
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("config_version: 3\nagent:\n  key: k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder

	if p, err := CreateProbeFragment(path, richProbe(), nil); err != nil {
		t.Fatalf("CreateProbeFragment: %v", err)
	} else {
		fmt.Fprintf(&log, "created %s\n", scrub(dir, p))
	}
	second := richProbe()
	second.Name = "second"
	second.Enabled = nil
	second.CustomTags = nil
	second.Governance = nil
	second.LogStrategies = nil
	second.Params = map[string]interface{}{"only": "one"}
	if _, err := CreateProbeFragment(path, second, nil); err != nil {
		t.Fatalf("CreateProbeFragment second: %v", err)
	}
	updated := richProbe()
	updated.Params["port"] = 5433
	updated.Params["added"] = []interface{}{map[string]interface{}{"a": 1}}
	if _, err := UpdateProbeFragment(path, updated, nil); err != nil {
		t.Fatalf("UpdateProbeFragment: %v", err)
	}

	if _, err := CreateStrategyFragment(path, "http", richStrategy(), true, nil); err != nil {
		t.Fatalf("CreateStrategyFragment: %v", err)
	}
	if _, err := CreateStrategyFragment(path, "otlp", StorageConfigParams{"endpoint": "c:4317", "insecure": true}, false, nil); err != nil {
		t.Fatalf("CreateStrategyFragment disabled: %v", err)
	}
	upd := richStrategy()
	upd["port"] = 9090
	if _, err := UpdateStrategyFragment(path, "http", upd, true, nil); err != nil {
		t.Fatalf("UpdateStrategyFragment: %v", err)
	}

	cfg, err := LoadFromDisk(path, nil)
	fmt.Fprintf(&log, "reload err=%v\n%s", err, scrub(dir, typedDump(cfg)))
	assertGolden(t, "fragment-writers.golden", log.String()+readTree(t, dir))
}

func TestYAMLGolden_SplitAndMigrate(t *testing.T) {
	useStubSecrets(t)
	var b strings.Builder
	for _, c := range repoCorpus(t) {
		if !c.monolithic {
			continue
		}
		dir := t.TempDir()
		path := c.stage(t, dir)
		res, err := MigrateToMultiFile(path, nil)
		fmt.Fprintf(&b, "##### %s\nerr=%s already=%v probes=%v strategies=%d\n%s", c.name,
			scrub(dir, fmt.Sprint(err)), res.AlreadyMultiFile, res.WroteProbes, res.StrategyCount, readTree(t, dir))
	}
	for _, c := range docSnippetCases(t) {
		if !c.monolithic {
			continue
		}
		dir := t.TempDir()
		path := c.stage(t, dir)
		res, err := MigrateToMultiFile(path, nil)
		out := readTree(t, dir)
		fmt.Fprintf(&b, "%s err=%t already=%v probes=%v strategies=%d %x\n", c.name, err != nil,
			res.AlreadyMultiFile, res.WroteProbes, res.StrategyCount, sha256.Sum256([]byte(scrub(dir, out))))
	}
	assertGolden(t, "split-migrate.golden", b.String())
}

func TestYAMLGolden_InstallAndEditors(t *testing.T) {
	mp := secret.NewMemoryProvider()
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	base := "# the agent\nconfig_version: 3\nagent:\n  key: \"\" # identity\n\ncache:\n  retention_minutes: 5\n"
	if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	step := func(name string, err error) { fmt.Fprintf(&log, "%s err=%s\n", name, scrub(dir, fmt.Sprint(err))) }

	step("ApplyInstallOverrides", ApplyInstallOverrides(path, "", map[string]string{"env": "prod", "site": "eu"}))
	step("SetLicenseField", SetLicenseField(path, "jwt-value"))
	step("SetAgentGovernance", SetAgentGovernance(path, map[string]interface{}{
		"criticality": "high",
		"owner":       map[string]interface{}{"team": "sre"},
		"labels":      []interface{}{"a", "b"},
	}))
	step("WriteOTLPStrategyFragment", WriteOTLPStrategyFragment(dir, "collector:4317", "grpc"))
	_, err := EnsureZabbixStrategyFragment(dir, "zbx.example.com", "linux")
	step("EnsureZabbixStrategyFragment", err)
	_, _, err = SetStrategyScalarReport(path, "otlp", "insecure", "true", "!!bool")
	step("SetStrategyScalarReport", err)
	step("EnsureAdminKey", EnsureAdminKey(path, nil))

	probeFile := filepath.Join(dir, "probes.d", "10-veeam.yaml")
	writeSealFile(t, probeFile, "- name: veeam-prod\n  type: veeam\n  params:\n    endpoint: https://vbr:9419\n    username: svc\n    password: my-plaintext-pw\n    director:\n      auth:\n        password: nested-pw\n    list: [a, b]\n")
	step("SealInlineSecrets", SealInlineSecrets(path, nil))
	step("MigrateLicenseToSidecar", MigrateLicenseToSidecar(path, nil))

	names, _ := mp.List()
	sort.Strings(names)
	fmt.Fprintf(&log, "secret names: %v\n", names)
	assertGolden(t, "install-editors.golden", log.String()+readTree(t, dir))
}

func TestYAMLGolden_SealMonolithic(t *testing.T) {
	mp := secret.NewMemoryProvider()
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	body := `config_version: 2
agent:
  key: "seal-key"
  license: "jwt-inline"
storage:
  - name: http
    params:
      port: 8080
      admin_key: "admin-plain"
      endpoints: ["prtg", "web"]
probes:
  - name: veeam-prod
    type: veeam
    params:
      endpoint: https://vbr:9419
      password: my-plaintext-pw
      tags:
        - x
        - y
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SealInlineSecrets(path, nil)
	names, _ := mp.List()
	sort.Strings(names)
	assertGolden(t, "seal-monolithic.golden", fmt.Sprintf("err=%s\nsecrets=%v\n%s", scrub(dir, fmt.Sprint(err)), names, readTree(t, dir)))
}
