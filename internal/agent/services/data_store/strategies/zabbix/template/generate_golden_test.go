package template

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

const generateGoldenPath = "testdata/generate.golden"

type anyLookups struct{}

func (anyLookups) Lookup(string) (map[int]string, bool) {
	return map[int]string{0: "No", 1: "Yes"}, true
}

func (anyLookups) Severities(string) (map[int]string, bool) {
	return map[int]string{0: "INFO", 1: "HIGH"}, true
}

// Every shipped probe definition, rendered for every platform and with
// and without a lookup source, must produce the same file byte for byte.
func TestGenerateGoldenOutput(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)

	variants := []struct {
		name string
		opts Options
	}{
		{"default", Options{}},
		{"linux", Options{Platform: "linux"}},
		{"windows", Options{Platform: "windows"}},
		{"lookups", Options{Platform: "linux", Lookups: anyLookups{}}},
		{"v6-prefix", Options{Version: "6.0", Prefix: "x", ItemDelay: "30s", DiscoveryDelay: "2m", Group: "G"}},
	}

	var sb strings.Builder
	for _, n := range names {
		for _, v := range variants {
			exp, gerr := Generate(defs[n], v.opts)
			if gerr != nil {
				fmt.Fprintf(&sb, "%s | %s | error: %v\n", n, v.name, gerr)
				continue
			}
			body, eerr := Encode(exp)
			if eerr != nil {
				fmt.Fprintf(&sb, "%s | %s | encode error: %v\n", n, v.name, eerr)
				continue
			}
			fmt.Fprintf(&sb, "%s | %s | %d bytes | %x\n", n, v.name, len(body), sha256.Sum256(body))
		}
	}
	got := sb.String()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(generateGoldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(generateGoldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(generateGoldenPath)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("Generate output changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
