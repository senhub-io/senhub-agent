// Package docsmetrics checks that the metric names a probe page prints are
// metrics the agent can actually emit.
//
// A page naming a metric that no definition declares is worse than a page
// saying nothing: a reader builds a dashboard panel or an alert rule on that
// name, gets an empty series, and has no way to tell a wrong name from a
// probe that collected nothing.
package docsmetrics

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// paramsBlock is the generated parameter table. Configuration keys legitimately
// look like metric names, so the block is removed before the page is read.
var paramsBlock = regexp.MustCompile(`(?s)<!-- schema:params:start -->.*?<!-- schema:params:end -->`)

// tableCode matches an identifier in backticks in the first cell of a table
// row: the shape every metric table on every page uses. The identifier must
// carry a dot, which is what separates a metric name from a bare word.
var tableCode = regexp.MustCompile("\\|\\s*`([a-z][a-z0-9_]*(?:\\.[a-z0-9_]+)+)`")

// metricSection matches the headings under which a page lists its metrics.
var metricSection = regexp.MustCompile(`(?i)(metric|channel)`)

// Names returns every metric name the definitions declare, in all the
// spellings a page may legitimately use: the internal name, the PRTG channel
// and the OTel name.
func Names() (map[string]bool, error) {
	defs, err := transformers.Definitions()
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			// A channel may carry a {placeholder}; the literal prefix is
			// what a page prints.
			out[strings.SplitN(s, "{", 2)[0]] = true
		}
	}
	for _, def := range defs {
		for _, m := range def.Metrics {
			add(m.Name)
			add(m.Channel)
			if m.Otel != nil {
				add(m.Otel.Name)
			}
		}
	}
	return out, nil
}

// Unknown is a metric name a page prints that no definition declares.
type Unknown struct {
	Page string
	Name string
}

// Audit reads every page under docsDir and reports the names printed in a
// metric table that no definition declares.
func Audit(docsDir string) ([]Unknown, error) {
	known, err := Names()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return nil, err
	}
	var out []Unknown
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(docsDir, e.Name())) // #nosec G304 - a documentation page of this repository
		if err != nil {
			return nil, err
		}
		text := paramsBlock.ReplaceAllString(string(raw), "")
		seen := map[string]bool{}
		for _, section := range metricSections(text) {
			for _, m := range tableCode.FindAllStringSubmatch(section, -1) {
				if known[m[1]] || seen[m[1]] {
					continue
				}
				seen[m[1]] = true
				out = append(out, Unknown{Page: e.Name(), Name: m[1]})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Page != out[j].Page {
			return out[i].Page < out[j].Page
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// metricSections returns the parts of a page that sit under a heading naming
// metrics or channels. A parameter table under "Configuration" is not one.
func metricSections(text string) []string {
	var out []string
	for _, part := range strings.Split(text, "\n#") {
		heading, _, _ := strings.Cut(part, "\n")
		if metricSection.MatchString(heading) {
			out = append(out, part)
		}
	}
	return out
}
