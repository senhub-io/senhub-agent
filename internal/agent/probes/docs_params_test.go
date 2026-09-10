package probes_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/docsparams"
)

// docsDir is where the user guide's probe pages live, relative to this
// package.
const docsDir = "../../../docs/user-guide/docs/probes"

// pageOverrides maps a probe type to its page when the file is not named
// after the type. Every entry is a page whose name predates the type.
var pageOverrides = map[string]string{
	"ping_gateway":         "ping-gateway.md",
	"ping_webapp":          "ping-webapp.md",
	"load_webapp":          "load-webapp.md",
	"wifi_signal_strength": "wifi-signal-strength.md",
	"snmp_poll":            "snmp-poll.md",
	"snmp_trap":            "snmp-trap.md",
	"os_updates":           "os-updates.md",
	"linux_logs":           "linux-logs.md",
	"windows_eventlog":     "windows-eventlog.md",
	"windows_services":     "windows-services.md",
	"logical_disk":         "logicaldisk.md",
	"otlp_receiver":        "otlp_receiver/README.md",
	"http_check":           "http-check.md",
	"phpfpm":               "php-fpm.md",
	"winservices":          "windows-services.md",
}

// pageFor returns the page of a probe type, or "" when it has none.
func pageFor(t *testing.T, probeType string) string {
	t.Helper()
	candidates := []string{probeType + ".md", strings.ReplaceAll(probeType, "_", "-") + ".md"}
	if o, ok := pageOverrides[probeType]; ok {
		candidates = append([]string{o}, candidates...)
	}
	for _, c := range candidates {
		p := filepath.Join(docsDir, c)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// TestProbePagesCarryTheirSchema keeps every page's parameter table equal
// to the schema its parser is guarded against. Run with UPDATE_DOCS=1 to
// rewrite the generated blocks (make docs-params).
func TestProbePagesCarryTheirSchema(t *testing.T) {
	update := os.Getenv("UPDATE_DOCS") == "1"
	var missing, stale []string

	for _, p := range spec.Registered() {
		page := pageFor(t, p.Type)
		if page == "" {
			missing = append(missing, p.Type)
			continue
		}
		raw, err := os.ReadFile(page) // #nosec G304 - path built from the registry
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		body := string(raw)
		want := docsparams.Render(p)

		i := strings.Index(body, docsparams.Start)
		j := strings.Index(body, docsparams.End)
		if i < 0 || j < i {
			if !update {
				stale = append(stale, p.Type+" ("+filepath.Base(page)+"): no generated block")
				continue
			}
			body = insertBlock(body, want)
		} else {
			got := body[i : j+len(docsparams.End)+1]
			if got == want {
				continue
			}
			if !update {
				stale = append(stale, p.Type+" ("+filepath.Base(page)+")")
				continue
			}
			body = body[:i] + want + body[j+len(docsparams.End)+1:]
		}
		if err := os.WriteFile(page, []byte(body), 0o600); err != nil {
			t.Fatalf("%s: %v", page, err)
		}
	}

	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("%d probe types have no documentation page: %s", len(missing), strings.Join(missing, ", "))
	}
	if len(stale) > 0 {
		t.Errorf("%d pages no longer match their schema; run make docs-params:\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
}

// insertBlock puts a generated block under the page's parameter heading,
// or at the end when it has none.
func insertBlock(body, block string) string {
	for _, heading := range []string{"\n### Parameters\n", "\n## Parameters\n", "\n## Configuration\n"} {
		if i := strings.Index(body, heading); i >= 0 {
			at := i + len(heading)
			return body[:at] + "\n" + block + body[at:]
		}
	}
	return strings.TrimRight(body, "\n") + "\n\n## Parameters\n\n" + block
}
