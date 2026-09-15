package docsparams

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/probes/spec"
)

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

// PageFor returns the page of a probe type under docsDir, or "" when it
// has none.
func PageFor(docsDir, probeType string) string {
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

// DocsDir returns the probe pages directory of the core checkout this
// package was compiled from. A probe package in another module resolves
// the documentation tree through it rather than guessing a relative path
// from its own repository: whichever core tree the build used — a
// sibling directory, a CI checkout, a workspace override — is the tree
// whose pages describe the schemas that were just compiled.
func DocsDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("docsparams: no compile-time path for this package")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "docs", "user-guide", "docs", "probes")
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("docsparams: probe pages not found at %s: %w", dir, err)
	}
	return filepath.Clean(dir), nil
}

// Result reports what a Sync found, sorted and ready to print.
type Result struct {
	// NoPage lists the probe types that have no documentation page.
	NoPage []string
	// Stale lists the pages whose generated block is absent or no longer
	// equal to the schema, as "type (page)". Empty after an updating run.
	Stale []string
	// Written lists the pages an updating run rewrote.
	Written []string
}

// Sync compares the generated block of every probe's page under docsDir
// with the block its schema renders to. With update true it rewrites the
// pages that differ and inserts the block in the pages that have none;
// with update false it only reports.
func Sync(docsDir string, probes []spec.Probe, update bool) (Result, error) {
	var res Result
	for _, p := range probes {
		page := PageFor(docsDir, p.Type)
		if page == "" {
			res.NoPage = append(res.NoPage, p.Type)
			continue
		}
		raw, err := os.ReadFile(page) // #nosec G304 - path built from the registry
		if err != nil {
			return res, fmt.Errorf("reading %s: %w", page, err)
		}
		body := strings.ReplaceAll(string(raw), "\r\n", "\n")
		want := Render(p)

		i := strings.Index(body, Start)
		j := strings.Index(body, End)
		switch {
		case i < 0 || j < i:
			if !update {
				res.Stale = append(res.Stale, p.Type+" ("+filepath.Base(page)+"): no generated block")
				continue
			}
			body = insertBlock(body, want)
		default:
			if body[i:j+len(End)+1] == want {
				continue
			}
			if !update {
				res.Stale = append(res.Stale, p.Type+" ("+filepath.Base(page)+")")
				continue
			}
			body = body[:i] + want + body[j+len(End)+1:]
		}
		if err := os.WriteFile(page, []byte(body), 0o600); err != nil {
			return res, fmt.Errorf("writing %s: %w", page, err)
		}
		res.Written = append(res.Written, filepath.Base(page))
	}
	sort.Strings(res.NoPage)
	sort.Strings(res.Stale)
	sort.Strings(res.Written)
	return res, nil
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
