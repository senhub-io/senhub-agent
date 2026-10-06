package configuration

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
)

func init() {
	spec.Register(spec.Probe{
		Type: "envtest_db", DisplayName: "Env test DB",
		Params: []spec.ParamSpec{
			{Key: "host", Kind: spec.KindString, Required: true},
			{Key: "port", Kind: spec.KindInt},
			{Key: "username", Kind: spec.KindString},
			{Key: "password", Kind: spec.KindString, Secret: true},
			{Key: "interval", Kind: spec.KindDuration},
			{Key: "verbose", Kind: spec.KindBool},
			{Key: "ratio", Kind: spec.KindFloat},
			{Key: "sslmode", Kind: spec.KindString, Enum: []string{"disable", "require"}},
			{Key: "databases", Kind: spec.KindStringList},
			{Key: "max_replication_lag_seconds", Kind: spec.KindInt},
			{Key: "tls", Kind: spec.KindBlock, Fields: []spec.ParamSpec{
				{Key: "ca_file", Kind: spec.KindString},
				{Key: "skip_verify", Kind: spec.KindBool, AlsoAccepts: []string{"insecure_skip_verify"}},
			}},
			{Key: "discovery", Kind: spec.KindBlock, Fields: []spec.ParamSpec{
				{Key: "interval", Kind: spec.KindDuration},
				{Key: "exclude", Kind: spec.KindStringList},
			}},
			{Key: "headers", Kind: spec.KindMap},
			{Key: "rules", Kind: spec.KindBlockList, Fields: []spec.ParamSpec{{Key: "name", Kind: spec.KindString}}},
			{Key: "state_file", Kind: spec.KindString},
			{Key: "enabled", Kind: spec.KindString},
		},
	})
}

func applyEnv(t *testing.T, data LocalConfigurationData, kv ...string) (LocalConfigurationData, error) {
	t.Helper()
	err := applyEnvProbes(&data, kv, nil)
	return data, err
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "v")
	writeFile(t, p, content)
	return p
}

func TestParseEnvProbes_Names(t *testing.T) {
	groups, err := parseEnvProbes([]string{
		"SENHUB_PROBE_PG_TYPE=envtest_db", "SENHUB_PROBE_pg_HOST=db", "SENHUB_PROBE_WEB2_TYPE=x",
		"SENHUB_PROBES=- name: a", "SENHUB_PROBE_EMPTY_HOST=", "PATH=/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].name != "pg" || groups[1].name != "web2" {
		t.Fatalf("groups = %+v", groups)
	}
	if len(groups[0].settings) != 2 {
		t.Fatalf("pg settings = %+v", groups[0].settings)
	}
}

func TestParseEnvProbes_Malformed(t *testing.T) {
	cases := map[string]string{
		"SENHUB_PROBE_PG=x":        "SENHUB_PROBE_PG",
		"SENHUB_PROBE__HOST=x":     "SENHUB_PROBE__HOST",
		"SENHUB_PROBE_PG_=x":       "SENHUB_PROBE_PG_",
		"SENHUB_PROBE_PG_A____B=x": "empty segment",
		"SENHUB_PROBE_PG_TLS__=x":  "empty segment",
		"SENHUB_PROBE_PG-X_HOST=x": "letters and digits",
	}
	for kv, want := range cases {
		_, err := parseEnvProbes([]string{kv})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want mention of %q", kv, err, want)
		}
	}
}

func TestEnvProbe_CreatesTypedProbe(t *testing.T) {
	data, err := applyEnv(t, LocalConfigurationData{},
		"SENHUB_PROBE_PG_TYPE=envtest_db",
		"SENHUB_PROBE_PG_HOST=db.example",
		"SENHUB_PROBE_PG_PORT=5433",
		"SENHUB_PROBE_PG_VERBOSE=yes",
		"SENHUB_PROBE_PG_RATIO=0.5",
		"SENHUB_PROBE_PG_INTERVAL=5m",
		"SENHUB_PROBE_PG_DATABASES=a, b ,c",
		"SENHUB_PROBE_PG_MAX_REPLICATION_LAG_SECONDS=30",
		"SENHUB_PROBE_PG_TLS__CA_FILE=/etc/ca.pem",
		"SENHUB_PROBE_PG_DISCOVERY__INTERVAL=90",
		"SENHUB_PROBE_PG_DISCOVERY__EXCLUDE=x*,y*",
		"SENHUB_PROBE_PG_HEADERS__X_REQUEST_NAME=abc",
		`SENHUB_PROBE_PG_RULES=[{"name":"r1"}]`,
		"SENHUB_PROBE_PG_ENABLED=false",
		"SENHUB_PROBE_PG_LOG_STRATEGIES=otlp",
		"SENHUB_PROBE_PG_CUSTOM_TAGS__ENV=prod",
		"SENHUB_PROBE_PG_GOVERNANCE__CRITICALITY=high",
		"SENHUB_PROBE_PG_GOVERNANCE__LABELS__APPLICATION=shop",
		"SENHUB_PROBE_PG_PARAMS__ENABLED=literal",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Probes) != 1 {
		t.Fatalf("probes = %+v", data.Probes)
	}
	p := data.Probes[0]
	if p.Name != "pg" || p.Type != "envtest_db" || p.IsEnabled() {
		t.Fatalf("probe = %+v", p)
	}
	want := map[string]interface{}{
		"host": "db.example", "port": 5433, "verbose": true, "ratio": 0.5,
		"interval": "5m", "databases": []interface{}{"a", "b", "c"},
		"max_replication_lag_seconds": 30,
		"tls":                         map[string]interface{}{"ca_file": "/etc/ca.pem"},
		"discovery":                   map[string]interface{}{"interval": 90, "exclude": []interface{}{"x*", "y*"}},
		"headers":                     map[string]interface{}{"x_request_name": "abc"},
		"rules":                       []interface{}{map[string]interface{}{"name": "r1"}},
		"enabled":                     "literal",
	}
	if !reflect.DeepEqual(p.Params, want) {
		t.Fatalf("params =\n%#v\nwant\n%#v", p.Params, want)
	}
	if !reflect.DeepEqual(p.LogStrategies, []string{"otlp"}) || p.CustomTags["env"] != "prod" {
		t.Fatalf("probe-level = %+v %+v", p.LogStrategies, p.CustomTags)
	}
	if p.Governance["criticality"] != "high" || p.Governance["labels"].(map[string]interface{})["application"] != "shop" {
		t.Fatalf("governance = %+v", p.Governance)
	}
}

func TestEnvProbe_Errors(t *testing.T) {
	base := []string{"SENHUB_PROBE_PG_TYPE=envtest_db", "SENHUB_PROBE_PG_HOST=h"}
	cases := []struct {
		name string
		env  []string
		want string
	}{
		{"unknown param", []string{"SENHUB_PROBE_PG_NOPE=1"}, "SENHUB_PROBE_PG_NOPE"},
		{"unknown nested", []string{"SENHUB_PROBE_PG_TLS__NOPE=1"}, "SENHUB_PROBE_PG_TLS__NOPE"},
		{"bad int", []string{"SENHUB_PROBE_PG_PORT=abc"}, "SENHUB_PROBE_PG_PORT"},
		{"bad bool", []string{"SENHUB_PROBE_PG_VERBOSE=maybe"}, "SENHUB_PROBE_PG_VERBOSE"},
		{"bad duration", []string{"SENHUB_PROBE_PG_INTERVAL=soon"}, "SENHUB_PROBE_PG_INTERVAL"},
		{"bad enum", []string{"SENHUB_PROBE_PG_SSLMODE=nope"}, "SENHUB_PROBE_PG_SSLMODE"},
		{"bad json", []string{"SENHUB_PROBE_PG_RULES={"}, "SENHUB_PROBE_PG_RULES"},
		{"block list nested", []string{"SENHUB_PROBE_PG_RULES__NAME=x"}, "SENHUB_PROBE_PG_RULES__NAME"},
		{"block as scalar", []string{"SENHUB_PROBE_PG_DISCOVERY=x"}, "SENHUB_PROBE_PG_DISCOVERY"},
		{"scalar with nesting", []string{"SENHUB_PROBE_PG_HOST__X=1"}, "SENHUB_PROBE_PG_HOST__X"},
		{"bad enabled", []string{"SENHUB_PROBE_PG_ENABLED=perhaps"}, "SENHUB_PROBE_PG_ENABLED"},
		{"bad log strategy", []string{"SENHUB_PROBE_PG_LOG_STRATEGIES=prtg"}, "SENHUB_PROBE_PG_LOG_STRATEGIES"},
		{"bad governance", []string{"SENHUB_PROBE_PG_GOVERNANCE__CRITICALITY=extreme"}, "SENHUB_PROBE_PG_GOVERNANCE__CRITICALITY"},
		{"both forms", []string{"SENHUB_PROBE_PG_USERNAME_FILE=/nonexistent", "SENHUB_PROBE_PG_USERNAME=x"}, "SENHUB_PROBE_PG_USERNAME"},
	}
	for _, c := range cases {
		_, err := applyEnv(t, LocalConfigurationData{}, append(append([]string{}, base...), c.env...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want mention of %q", c.name, err, c.want)
		}
	}
}

func TestEnvProbe_DeclarationErrors(t *testing.T) {
	if _, err := applyEnv(t, LocalConfigurationData{}, "SENHUB_PROBE_PG_HOST=h"); err == nil || !strings.Contains(err.Error(), "SENHUB_PROBE_PG_TYPE") {
		t.Errorf("no type: %v", err)
	}
	SetProbeTypeLookup(func(string) bool { return false })
	t.Cleanup(func() { SetProbeTypeLookup(nil) })
	if _, err := applyEnv(t, LocalConfigurationData{}, "SENHUB_PROBE_PG_TYPE=nosuchtype"); err == nil || !strings.Contains(err.Error(), "nosuchtype") {
		t.Errorf("unknown type: %v", err)
	}
	if _, err := applyEnv(t, LocalConfigurationData{}, "SENHUB_PROBE_PG_TYPE=envtest_db"); err == nil || !strings.Contains(err.Error(), "host") {
		t.Errorf("missing required: %v", err)
	}
}

func TestEnvProbe_NoSchemaTypeTakesStrings(t *testing.T) {
	SetProbeTypeLookup(func(tp string) bool { return tp == "schemaless" })
	t.Cleanup(func() { SetProbeTypeLookup(nil) })
	data, err := applyEnv(t, LocalConfigurationData{},
		"SENHUB_PROBE_X_TYPE=schemaless", "SENHUB_PROBE_X_PORT=80", "SENHUB_PROBE_X_OPTS__DEEP_KEY=v", "SENHUB_PROBE_X_API_TOKEN=s3")
	if err != nil {
		t.Fatal(err)
	}
	got := data.Probes[0].Params
	if got["port"] != "80" || got["opts"].(map[string]interface{})["deep_key"] != "v" {
		t.Fatalf("params = %#v", got)
	}
	if got["api_token"] != "${env:SENHUB_PROBE_X_API_TOKEN}" {
		t.Fatalf("a sensitive key must stay a reference, got %#v", got["api_token"])
	}
}

func TestEnvProbe_OverridesFileProbe(t *testing.T) {
	file := LocalConfigurationData{Probes: []ProbeConfig{{
		Name: "my-pg", Type: "envtest_db",
		Params: map[string]interface{}{
			"host": "filehost", "port": 5432, "username": "mon",
			"discovery": map[string]interface{}{"interval": 300, "exclude": []interface{}{"keep"}},
		},
	}, {Name: "other", Type: "envtest_db", Params: map[string]interface{}{"host": "o"}}}}
	data, err := applyEnv(t, file,
		"SENHUB_PROBE_MYPG_HOST=envhost", "SENHUB_PROBE_MYPG_DISCOVERY__INTERVAL=60")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Probes) != 2 {
		t.Fatalf("probes = %d, a same-named probe must not be duplicated", len(data.Probes))
	}
	p := data.Probes[0].Params
	if p["host"] != "envhost" || p["port"] != 5432 || p["username"] != "mon" {
		t.Fatalf("params = %#v", p)
	}
	d := p["discovery"].(map[string]interface{})
	if d["interval"] != 60 || !reflect.DeepEqual(d["exclude"], []interface{}{"keep"}) {
		t.Fatalf("nested merge = %#v", d)
	}
	if data.Probes[1].Params["host"] != "o" {
		t.Fatal("another probe was touched")
	}
}

func TestEnvProbe_TypeMismatchAndAmbiguity(t *testing.T) {
	file := LocalConfigurationData{Probes: []ProbeConfig{{Name: "pg", Type: "envtest_db", Params: map[string]interface{}{"host": "h"}}}}
	if _, err := applyEnv(t, file, "SENHUB_PROBE_PG_TYPE=other"); err == nil || !strings.Contains(err.Error(), "SENHUB_PROBE_PG_TYPE") {
		t.Errorf("type mismatch: %v", err)
	}
	if _, err := applyEnv(t, file, "SENHUB_PROBE_PG_TYPE=envtest_db"); err != nil {
		t.Errorf("same type must be accepted: %v", err)
	}
	two := LocalConfigurationData{Probes: []ProbeConfig{{Name: "a-b", Type: "envtest_db"}, {Name: "a_b", Type: "envtest_db"}}}
	if _, err := applyEnv(t, two, "SENHUB_PROBE_AB_HOST=h"); err == nil || !strings.Contains(err.Error(), "two probes") {
		t.Errorf("ambiguous name: %v", err)
	}
}

func TestEnvProbe_FileSuffix(t *testing.T) {
	dir := t.TempDir()
	pw := writeTemp(t, "s3cret\n")
	ca := filepath.Join(dir, "ca")
	writeFile(t, ca, "/x/ca.pem\n")

	data, err := applyEnv(t, LocalConfigurationData{},
		"SENHUB_PROBE_PG_TYPE=envtest_db", "SENHUB_PROBE_PG_HOST=h",
		"SENHUB_PROBE_PG_PASSWORD_FILE="+pw,
		"SENHUB_PROBE_PG_PORT_FILE="+writeTemp(t, "6000\n"),
		// ca_file is a key of its own: the exact name wins over the suffix.
		"SENHUB_PROBE_PG_TLS__CA_FILE="+ca,
		"SENHUB_PROBE_PG_STATE_FILE_FILE="+ca,
	)
	if err != nil {
		t.Fatal(err)
	}
	p := data.Probes[0].Params
	if p["password"] != "${file:"+pw+"}" {
		t.Fatalf("a secret read from a file stays a reference, got %#v", p["password"])
	}
	if p["port"] != 6000 {
		t.Fatalf("port = %#v", p["port"])
	}
	if p["tls"].(map[string]interface{})["ca_file"] != ca {
		t.Fatalf("tls.ca_file = %#v", p["tls"])
	}
	if p["state_file"] != "/x/ca.pem" {
		t.Fatalf("state_file = %#v", p["state_file"])
	}

	for name, kv := range map[string]string{
		"missing": "SENHUB_PROBE_PG_PASSWORD_FILE=" + filepath.Join(dir, "absent"),
		"empty":   "SENHUB_PROBE_PG_PASSWORD_FILE=" + writeTemp(t, "\n"),
	} {
		_, err := applyEnv(t, LocalConfigurationData{}, "SENHUB_PROBE_PG_TYPE=envtest_db", "SENHUB_PROBE_PG_HOST=h", kv)
		if err == nil || !strings.Contains(err.Error(), "SENHUB_PROBE_PG_PASSWORD_FILE") {
			t.Errorf("%s file: %v", name, err)
		}
	}
}

func TestEnvProbe_DollarInValueSurvivesSubstitution(t *testing.T) {
	data, err := applyEnv(t, LocalConfigurationData{},
		"SENHUB_PROBE_PG_TYPE=envtest_db", "SENHUB_PROBE_PG_HOST=h", "SENHUB_PROBE_PG_USERNAME=a$b${env:HOME}")
	if err != nil {
		t.Fatal(err)
	}
	if err := Substitute(&data); err != nil {
		t.Fatal(err)
	}
	if got := data.Probes[0].Params["username"]; got != "a$b${env:HOME}" {
		t.Fatalf("username = %#v", got)
	}
}

func TestEnvProbe_JSONOfAMap(t *testing.T) {
	data, err := applyEnv(t, LocalConfigurationData{},
		"SENHUB_PROBE_PG_TYPE=envtest_db", "SENHUB_PROBE_PG_HOST=h",
		`SENHUB_PROBE_PG_HEADERS={"Authorization":"Bearer x","N":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	h := data.Probes[0].Params["headers"].(map[string]interface{})
	if h["Authorization"] != "Bearer x" {
		t.Fatalf("headers = %#v", h)
	}
}

// End to end: a configuration holding no probe file, only the environment.
func TestLoadFromDisk_OnlyEnvProbes(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeFile(t, cfg, "config_version: 3\nagent:\n  key: k\n")
	t.Setenv("SENHUB_PROBE_PG_TYPE", "envtest_db")
	t.Setenv("SENHUB_PROBE_PG_HOST", "db.example")
	t.Setenv("SENHUB_PROBE_PG_PASSWORD", "hunter2")
	t.Setenv("SENHUB_PROBE_PG_PORT", "5433")

	data, err := LoadFromDisk(cfg, loaderTestLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Probes) != 1 {
		t.Fatalf("probes = %+v", data.Probes)
	}
	p := data.Probes[0]
	if p.Name != "pg" || p.Params["host"] != "db.example" || p.Params["port"] != 5433 || p.Params["password"] != "hunter2" {
		t.Fatalf("probe = %+v", p)
	}
}

func TestLoadFromDisk_EnvOverridesFragmentAndLegacy(t *testing.T) {
	t.Setenv("SENHUB_PROBE_PG_HOST", "envhost")
	for name, setup := range map[string]func(dir string) string{
		"multi-file": func(dir string) string {
			writeFile(t, filepath.Join(dir, "probes.d", "10-pg.yaml"), "- name: pg\n  type: envtest_db\n  params: {host: filehost, port: 1}\n")
			writeFile(t, filepath.Join(dir, "agent.yaml"), "config_version: 3\n")
			return filepath.Join(dir, "agent.yaml")
		},
		"legacy": func(dir string) string {
			writeFile(t, filepath.Join(dir, "agent.yaml"), "config_version: 2\nprobes:\n  - name: pg\n    type: envtest_db\n    params: {host: filehost, port: 1}\n")
			return filepath.Join(dir, "agent.yaml")
		},
	} {
		data, err := LoadFromDisk(setup(t.TempDir()), loaderTestLogger(t))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(data.Probes) != 1 || data.Probes[0].Params["host"] != "envhost" || data.Probes[0].Params["port"] != 1 {
			t.Fatalf("%s: probes = %+v", name, data.Probes)
		}
	}
}

func TestLoadFromDisk_EnvErrorNamesTheVariable(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeFile(t, cfg, "config_version: 3\n")
	t.Setenv("SENHUB_PROBE_PG_TYPE", "envtest_db")
	t.Setenv("SENHUB_PROBE_PG_HOST", "h")
	t.Setenv("SENHUB_PROBE_PG_PORT", "many")
	_, err := LoadFromDisk(cfg, loaderTestLogger(t))
	if err == nil || !strings.Contains(err.Error(), "SENHUB_PROBE_PG_PORT") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigShow_EnvProbeSecretsAreNeverPrinted(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeFile(t, cfg, "config_version: 3\n")
	pwFile := writeTemp(t, "from-file-secret\n")
	t.Setenv("SENHUB_PROBE_PG_TYPE", "envtest_db")
	t.Setenv("SENHUB_PROBE_PG_HOST", "db.example")
	t.Setenv("SENHUB_PROBE_PG_PASSWORD", "from-env-secret")

	render := func(mode ShowMode) string {
		data, err := LoadForShow(cfg, mode, nil)
		if err != nil {
			t.Fatal(err)
		}
		out, err := MarshalSortedYAML(&data)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	for _, mode := range []ShowMode{ShowRedact, ShowRaw} {
		if out := render(mode); strings.Contains(out, "from-env-secret") {
			t.Errorf("mode %d prints the secret:\n%s", mode, out)
		}
	}
	if out := render(ShowRaw); !strings.Contains(out, "${env:SENHUB_PROBE_PG_PASSWORD}") {
		t.Errorf("raw should show the reference:\n%s", out)
	}
	if out := render(ShowResolved); !strings.Contains(out, "from-env-secret") {
		t.Errorf("resolved should carry the value:\n%s", out)
	}
	if out := render(ShowRedact); !strings.Contains(out, "host: db.example") || !strings.Contains(out, "***") {
		t.Errorf("redact should keep plain values and mask the secret:\n%s", out)
	}

	t.Setenv("SENHUB_PROBE_PG_PASSWORD", "")
	t.Setenv("SENHUB_PROBE_PG_PASSWORD_FILE", pwFile)
	for _, mode := range []ShowMode{ShowRedact, ShowRaw} {
		if out := render(mode); strings.Contains(out, "from-file-secret") {
			t.Errorf("mode %d prints the file secret:\n%s", mode, out)
		}
	}
	if out := render(ShowResolved); !strings.Contains(out, "from-file-secret") {
		t.Errorf("resolved should carry the file value:\n%s", out)
	}
}

func TestEnvProbeVariables_NamesOnly(t *testing.T) {
	t.Setenv("SENHUB_PROBE_B_HOST", "x")
	t.Setenv("SENHUB_PROBE_A_HOST", "y")
	t.Setenv("SENHUB_PROBE_C_HOST", "")
	got := EnvProbeVariables()
	want := []string{"SENHUB_PROBE_A_HOST", "SENHUB_PROBE_B_HOST"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
