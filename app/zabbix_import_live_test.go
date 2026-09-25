package app

import (
	"os"
	"sort"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// TestEveryTemplateImportsIntoALiveZabbix is the part of the template
// guard that only a server can give: it imports every generated template,
// both platforms and both export formats, into the Zabbix named by
// ZABBIX_URL, and fails on the first refusal.
//
// It is skipped without a server. Run it with `make test-zabbix-import`
// against a lab; the templates it imports update the ones already there,
// so it is safe to repeat.
//
// Authentication takes ZABBIX_API_TOKEN, or ZABBIX_USER and ZABBIX_PASSWORD
// for a session login. Both lines, 7.0 and 8.0, accept the session token
// as a bearer.
func TestEveryTemplateImportsIntoALiveZabbix(t *testing.T) {
	url := os.Getenv("ZABBIX_URL")
	if url == "" {
		t.Skip("ZABBIX_URL not set; see make test-zabbix-import")
	}
	token := os.Getenv("ZABBIX_API_TOKEN")
	api := newZabbixAPI(url, token)
	if token == "" {
		user, pass := os.Getenv("ZABBIX_USER"), os.Getenv("ZABBIX_PASSWORD")
		if user == "" || pass == "" {
			t.Fatal("set ZABBIX_API_TOKEN, or ZABBIX_USER and ZABBIX_PASSWORD")
		}
		var session string
		if err := api.call("user.login", map[string]string{"username": user, "password": pass}, &session); err != nil {
			t.Fatalf("logging in to %s: %v", url, err)
		}
		api.token = session
	}
	var version string
	if err := api.call("apiinfo.version", []string{}, &version); err != nil {
		t.Fatalf("reading the server version: %v", err)
	}
	t.Logf("Zabbix %s at %s", version, url)

	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)

	rules := map[string]interface{}{
		"templates": map[string]bool{"createMissing": true, "updateExisting": true},
		// The rule is named template_groups on every server line, whatever
		// tag the imported file itself uses.
		"template_groups": map[string]bool{"createMissing": true},
		"items":           map[string]bool{"createMissing": true, "updateExisting": true},
		"discoveryRules":  map[string]bool{"createMissing": true, "updateExisting": true},
		"valueMaps":       map[string]bool{"createMissing": true, "updateExisting": true},
	}
	imported := 0
	for _, probe := range names {
		for _, platform := range []string{"", "linux", "windows"} {
			for _, exportVersion := range []string{"6.0", "7.0"} {
				exp, err := template.Generate(defs[probe], template.Options{Platform: platform, Version: exportVersion})
				if err != nil {
					t.Fatalf("%s: %v", probe, err)
				}
				if exp.DeclaresNothing() {
					continue
				}
				body, err := template.Encode(exp)
				if err != nil {
					t.Fatalf("%s: %v", probe, err)
				}
				err = api.call("configuration.import", map[string]interface{}{
					"format": "yaml", "source": string(body), "rules": rules,
				}, nil)
				if err != nil {
					t.Errorf("%s (%s, export %s) refused by Zabbix %s: %v", probe, platform, exportVersion, version, err)
					continue
				}
				imported++
			}
		}
	}
	t.Logf("%d templates imported into Zabbix %s", imported, version)
}
