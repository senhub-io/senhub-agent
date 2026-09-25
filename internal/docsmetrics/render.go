package docsmetrics

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Start and End delimit the generated metric reference on a probe page.
const (
	Start = "<!-- schema:metrics:start -->"
	End   = "<!-- schema:metrics:end -->"
)

// Render builds the complete metric reference of one probe from its
// definition: the OTel name every pull and push output uses, the channel the
// PRTG and Nagios outputs carry, the unit and the description.
//
// The curated sections above it group metrics and explain them; this table
// exists so the page is exhaustive, which no hand-written table stayed.
func Render(def transformers.ProbeDefinition) string {
	var b strings.Builder
	b.WriteString(Start + "\n")
	b.WriteString("<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->\n\n")
	b.WriteString("| Metric | Channel | Unit | Description |\n|---|---|---|---|\n")
	for _, m := range def.Metrics {
		otel := "-"
		if m.Otel != nil && m.Otel.Name != "" && !m.Otel.Skip {
			otel = "`" + m.Otel.Name + "`"
		}
		channel := m.Channel
		if channel == "" {
			channel = m.Name
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n",
			otel, channel, cell(m.Unit), cell(m.Description))
	}
	b.WriteString("\n" + End + "\n")
	return b.String()
}

// cell makes a definition field safe inside a Markdown table.
func cell(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		return "-"
	}
	return s
}

// Result reports what a Sync found.
type Result struct {
	// NoPage lists the probes whose definition has no documentation page.
	NoPage []string
	// Stale lists the pages whose block is absent or no longer equal to the
	// definition. Empty after an updating run.
	Stale []string
	// Written lists the pages an updating run rewrote.
	Written []string
}

// Sync compares the generated block of every page with what its definition
// renders to; with update it rewrites the pages that differ.
func Sync(docsDir string, update bool) (Result, error) {
	var res Result
	defs, err := transformers.Definitions()
	if err != nil {
		return res, err
	}
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, probe := range names {
		page := pageFor(docsDir, probe)
		if page == "" {
			res.NoPage = append(res.NoPage, probe)
			continue
		}
		raw, err := os.ReadFile(page) // #nosec G304 - path built from the definition registry
		if err != nil {
			return res, fmt.Errorf("reading %s: %w", page, err)
		}
		body := strings.ReplaceAll(string(raw), "\r\n", "\n")
		want := Render(defs[probe])

		i := strings.Index(body, Start)
		j := strings.Index(body, End)
		switch {
		case i < 0 || j < i:
			if !update {
				res.Stale = append(res.Stale, probe+" ("+filepath.Base(page)+"): no generated block")
				continue
			}
			body = strings.TrimRight(body, "\n") + "\n\n## Metric reference\n\nEvery metric this probe can emit. The first column is the name the\nOTLP and Prometheus outputs use, the second the channel the PRTG and\nNagios outputs carry.\n\n" + want
		default:
			if body[i:j+len(End)+1] == want {
				continue
			}
			if !update {
				res.Stale = append(res.Stale, probe+" ("+filepath.Base(page)+")")
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

// pageOverrides maps a probe whose page is not named after it.
var pageOverrides = map[string]string{
	"phpfpm":      "php-fpm.md",
	"winservices": "windows-services.md",
}

func pageFor(docsDir, probe string) string {
	candidates := []string{probe + ".md", strings.ReplaceAll(probe, "_", "-") + ".md"}
	if o, ok := pageOverrides[probe]; ok {
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
