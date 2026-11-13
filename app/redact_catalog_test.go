package app

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
	"senhub-agent.go/internal/agent/services/logger"
)

var catalogURLKeyPattern = regexp.MustCompile(`(?i)(url|uri|endpoint|dsn|address|target)`)
var witnessPattern = regexp.MustCompile(`WITNESS-[A-Z]+-[^@\\s\"']+`)
var leakPattern = regexp.MustCompile(`WITNESS-[A-Za-z0-9_.-]+`)

type catalogLeaf struct {
	key   string
	value string
}

type catalogEntry struct {
	kind    string
	typ     string
	params  map[string]interface{}
	secrets int
	urls    int
	leaves  []catalogLeaf
}

func catalogWitnessParams(fields []spec.ParamSpec, typeName, prefix string, secrets, urls *int, leaves *[]catalogLeaf) map[string]interface{} {
	params := map[string]interface{}{}
	for _, f := range fields {
		path := f.Key
		if prefix != "" {
			path = prefix + "." + f.Key
		}
		switch f.Kind {
		case spec.KindBlock:
			params[f.Key] = catalogWitnessParams(f.Fields, typeName, path, secrets, urls, leaves)
		case spec.KindBlockList:
			params[f.Key] = []interface{}{catalogWitnessParams(f.Fields, typeName, path, secrets, urls, leaves)}
		case spec.KindMap:
			if f.Secret {
				*secrets++
				w := "WITNESS-SECRET-" + typeName + "-" + path
				params[f.Key] = map[string]interface{}{"X-Witness": w}
				// A header name carries no secret signal in free text; the
				// view and log rails check this map.
			}
		case spec.KindStringList:
			if f.Secret {
				*secrets++
				w := "WITNESS-SECRET-" + typeName + "-" + path
				params[f.Key] = []interface{}{w}
				*leaves = append(*leaves, catalogLeaf{key: path, value: w})
			} else if catalogURLKeyPattern.MatchString(f.Key) {
				*urls++
				u := "https://wuser:WITNESS-URL-" + typeName + "-" + path + "@host.example/p"
				params[f.Key] = []interface{}{u}
				*leaves = append(*leaves, catalogLeaf{key: path, value: u})
			}
		default:
			if f.Secret {
				*secrets++
				w := "WITNESS-SECRET-" + typeName + "-" + path
				params[f.Key] = w
				*leaves = append(*leaves, catalogLeaf{key: path, value: w})
			} else if catalogURLKeyPattern.MatchString(f.Key) {
				*urls++
				u := "https://wuser:WITNESS-URL-" + typeName + "-" + path + "@host.example/p"
				params[f.Key] = u
				*leaves = append(*leaves, catalogLeaf{key: path, value: u})
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

	var entries []catalogEntry
	totalTypes, totalSecrets, totalURLs := 0, 0, 0
	for _, p := range spec.Registered() {
		secrets, urls := 0, 0
		var leaves []catalogLeaf
		params := catalogWitnessParams(p.Params, p.Type, "", &secrets, &urls, &leaves)
		entry := map[string]interface{}{"name": "w-" + p.Type, "type": p.Type, "params": params}
		data, err := yaml.Marshal([]interface{}{entry})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-"+p.Type+".yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, catalogEntry{kind: "probe", typ: p.Type, params: params, secrets: secrets, urls: urls, leaves: leaves})
		totalTypes++
		totalSecrets += secrets
		totalURLs += urls
	}
	for _, o := range outputspec.Registered() {
		secrets, urls := 0, 0
		var leaves []catalogLeaf
		params := catalogWitnessParams(o.Params, o.Type, "", &secrets, &urls, &leaves)
		data, err := yaml.Marshal(map[string]interface{}{o.Type: params})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "strategies.d", "10-"+o.Type+".yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, catalogEntry{kind: "output", typ: o.Type, params: params, secrets: secrets, urls: urls, leaves: leaves})
		totalTypes++
		totalSecrets += secrets
		totalURLs += urls
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
	viewOK := true
	for _, text := range []string{string(shown), remote.String()} {
		for _, w := range witnessPattern.FindAllString(text, -1) {
			viewOK = false
			t.Errorf("view rail leaked for catalogue: %s", w)
		}
	}

	for _, e := range entries {
		var buf bytes.Buffer
		l := zerolog.New(logger.NewMaskingWriter(&buf))
		if e.kind == "probe" {
			l.Info().Str("probe_name", "w-"+e.typ).Any("probe_params", configuration.SanitizeProbeParamsForLog(e.typ, e.params)).Msg("Starting new probe")
		} else {
			l.Info().Any("old_params", configuration.SanitizeOutputParamsForLog(e.typ, e.params)).Any("new_params", configuration.SanitizeOutputParamsForLog(e.typ, e.params)).Msg("Strategy configuration changed, attempting update")
		}
		logOK := !strings.Contains(buf.String(), "WITNESS-")
		if !logOK {
			for _, w := range leakPattern.FindAllString(buf.String(), -1) {
				t.Errorf("log rail leaked for %s %s: %s", e.kind, e.typ, w)
			}
		}

		// No code formats a parameter map into a message; the log rail above
		// covers parameters, which only reach a log through the sanitizer.
		// What an error carries is a value quoted as is (a URL, a DSN) or a
		// named key=value pair: that is what this rail checks.
		errOK := true
		for _, lf := range e.leaves {
			msgs := []string{lf.key + "=" + lf.value, lf.key + ": " + lf.value}
			if strings.HasPrefix(lf.value, "https://") {
				msgs = append(msgs, "dial "+lf.value+": connection refused")
			}
			for _, s := range msgs {
				r := configuration.RedactSecretsInString(s)
				if strings.Contains(r, "WITNESS-") {
					errOK = false
					for _, w := range leakPattern.FindAllString(r, -1) {
						t.Errorf("error rail leaked for %s %s: %s", e.kind, e.typ, w)
					}
				}
			}
		}

		viewStatus, logStatus, errStatus := "ok", "ok", "ok"
		if !viewOK {
			viewStatus = "LEAK"
		}
		if !logOK {
			logStatus = "LEAK"
		}
		if !errOK {
			errStatus = "LEAK"
		}
		t.Logf("%s %s: %d secret fields, %d URL fields, view %s, log %s, error %s", e.kind, e.typ, e.secrets, e.urls, viewStatus, logStatus, errStatus)
	}

	if totalTypes == 0 || totalSecrets == 0 || totalURLs == 0 {
		t.Fatalf("catalogue walk found nothing to check: %d types, %d secret fields, %d URL fields", totalTypes, totalSecrets, totalURLs)
	}
	t.Logf("catalogue: %d types, %d secret fields, %d URL fields, 3 rails (view, log, error), no witness leaked", totalTypes, totalSecrets, totalURLs)
}
