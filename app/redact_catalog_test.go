package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

var catalogURLKeyPattern = regexp.MustCompile(`(?i)(url|uri|endpoint|dsn|address|target)`)
var witnessPattern = regexp.MustCompile(`WITNESS-[A-Z]+-[^@\\s\"']+`)

// catalogWitnessParams builds a params map holding a witness value in
// every secret field and a witness URL in every URL-ish string field.
func catalogWitnessParams(fields []spec.ParamSpec, typeName, prefix string, secrets, urls *int) map[string]interface{} {
	params := map[string]interface{}{}
	for _, f := range fields {
		path := f.Key
		if prefix != "" {
			path = prefix + "." + f.Key
		}
		switch f.Kind {
		case spec.KindBlock:
			params[f.Key] = catalogWitnessParams(f.Fields, typeName, path, secrets, urls)
		case spec.KindBlockList:
			params[f.Key] = []interface{}{catalogWitnessParams(f.Fields, typeName, path, secrets, urls)}
		case spec.KindMap:
			if f.Secret {
				*secrets++
				params[f.Key] = map[string]interface{}{"X-Witness": "WITNESS-SECRET-" + typeName + "-" + path}
			}
		case spec.KindStringList:
			if f.Secret {
				*secrets++
				params[f.Key] = []interface{}{"WITNESS-SECRET-" + typeName + "-" + path}
			} else if catalogURLKeyPattern.MatchString(f.Key) {
				*urls++
				params[f.Key] = []interface{}{"https://wuser:WITNESS-URL-" + typeName + "-" + path + "@host.example/p"}
			}
		default:
			if f.Secret {
				*secrets++
				params[f.Key] = "WITNESS-SECRET-" + typeName + "-" + path
			} else if catalogURLKeyPattern.MatchString(f.Key) {
				*urls++
				params[f.Key] = "https://wuser:WITNESS-URL-" + typeName + "-" + path + "@host.example/p"
			}
		}
	}
	return params
}

func TestRedactionCoversTheWholeCatalogue(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("config_version: 3\nagent:\n  name: catalogue\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	totalTypes, totalSecrets, totalURLs := 0, 0, 0
	for _, p := range spec.Registered() {
		secrets, urls := 0, 0
		params := catalogWitnessParams(p.Params, p.Type, "", &secrets, &urls)
		entry := map[string]interface{}{"name": "w-" + p.Type, "type": p.Type, "params": params}
		data, err := yaml.Marshal([]interface{}{entry})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-"+p.Type+".yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		totalTypes++
		totalSecrets += secrets
		totalURLs += urls
		t.Logf("probe %s: %d secret fields, %d URL fields, ok", p.Type, secrets, urls)
	}
	for _, o := range outputspec.Registered() {
		secrets, urls := 0, 0
		params := catalogWitnessParams(o.Params, o.Type, "", &secrets, &urls)
		data, err := yaml.Marshal(map[string]interface{}{o.Type: params})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "strategies.d", "10-"+o.Type+".yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		totalTypes++
		totalSecrets += secrets
		totalURLs += urls
		t.Logf("output %s: %d secret fields, %d URL fields, ok", o.Type, secrets, urls)
	}

	cfg, err := configuration.LoadForShow(filepath.Join(dir, "agent.yaml"), configuration.ShowRedact, nil)
	if err != nil {
		t.Fatalf("LoadForShow: %v", err)
	}
	shown, err := configuration.MarshalSortedYAML(cfg)
	if err != nil {
		t.Fatalf("MarshalSortedYAML: %v", err)
	}
	files, err := configuration.RedactedConfigFiles(filepath.Join(dir, "agent.yaml"), nil)
	if err != nil {
		t.Fatalf("RedactedConfigFiles: %v", err)
	}
	var remote strings.Builder
	for _, f := range files {
		remote.Write(f.Body)
	}
	for _, text := range []string{string(shown), remote.String()} {
		for _, w := range witnessPattern.FindAllString(text, -1) {
			t.Errorf("witness leaked in redacted configuration: %s", w)
		}
	}
	if totalTypes == 0 || totalSecrets == 0 || totalURLs == 0 {
		t.Fatalf("catalogue walk found nothing to check: %d types, %d secret fields, %d URL fields", totalTypes, totalSecrets, totalURLs)
	}
	t.Logf("catalogue: %d types, %d secret fields, %d URL fields, no witness leaked", totalTypes, totalSecrets, totalURLs)
}
