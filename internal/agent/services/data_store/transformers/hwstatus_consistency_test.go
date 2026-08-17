package transformers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// One OTel metric name must have ONE instrument type across every probe that
// emits it. hw.status was declared as a gauge by ipmi and an updowncounter by
// redfish, so a backend receiving both saw the same name arrive as a Gauge from
// one host and a Sum from another — generally rejected, or silently kept as
// whichever shape arrived first (#792).
//
// This walks every definition rather than pinning hw.status alone: the defect
// is a class, and the next collision should fail here rather than in a
// customer's backend.
func TestOTelMetricNameHasOneInstrumentTypeEverywhere(t *testing.T) {
	files, err := filepath.Glob("definitions/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no transformer definitions found: %v", err)
	}

	otelBlock := regexp.MustCompile(`(?m)^\s{4}otel:\s*$`)
	nameLine := regexp.MustCompile(`^\s+name:\s*(\S+)\s*$`)
	typeLine := regexp.MustCompile(`^\s+type:\s*(\w+)\s*$`)

	// otel name -> instrument type -> the files that declare it that way
	seen := map[string]map[string][]string{}

	for _, f := range files {
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			t.Fatalf("reading %s: %v", f, readErr)
		}
		lines := strings.Split(string(data), "\n")
		for i := 0; i < len(lines); i++ {
			if !otelBlock.MatchString(lines[i]) {
				continue
			}
			var name, typ string
			for j := i + 1; j < len(lines); j++ {
				if !regexp.MustCompile(`^\s{6,}\w`).MatchString(lines[j]) {
					break
				}
				if m := nameLine.FindStringSubmatch(lines[j]); m != nil && name == "" {
					name = m[1]
				}
				if m := typeLine.FindStringSubmatch(lines[j]); m != nil && typ == "" {
					typ = m[1]
				}
			}
			if name == "" || typ == "" {
				continue
			}
			if seen[name] == nil {
				seen[name] = map[string][]string{}
			}
			base := filepath.Base(f)
			if !contains(seen[name][typ], base) {
				seen[name][typ] = append(seen[name][typ], base)
			}
		}
	}

	for name, byType := range seen {
		if len(byType) <= 1 {
			continue
		}
		var detail []string
		for typ, files := range byType {
			detail = append(detail, typ+" in "+strings.Join(files, ", "))
		}
		t.Errorf("OTel metric %q is declared with %d different instrument types: %s\n"+
			"A backend receiving both sees one name arrive as two shapes.",
			name, len(byType), strings.Join(detail, "; "))
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
