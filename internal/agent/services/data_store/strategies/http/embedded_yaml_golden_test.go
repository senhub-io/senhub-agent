package http

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// TestEmbeddedYAMLDecodesToTheSameValues pins what the embedded lookups and the
// embedded Nagios configuration decode to, so a change of YAML library shows
// up here. Run with UPDATE_YAML_GOLDEN=1 to rewrite the golden file.
func TestEmbeddedYAMLDecodesToTheSameValues(t *testing.T) {
	var lines []string
	digest := func(name string, v interface{}) {
		body, err := json.Marshal(v)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s JSONERR %v", name, err))
			return
		}
		lines = append(lines, fmt.Sprintf("%s %x %d", name, sha256.Sum256(body), len(body)))
	}

	zl := zerolog.Nop()
	registry, err := NewLookupRegistry(&zl)
	if err != nil {
		t.Fatalf("lookups: %v", err)
	}
	all := registry.GetAllLookups()
	digest("lookups", all)
	digest("lookups/count", len(all))

	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())
	nagios, err := cm.loadEmbeddedNagiosConfig()
	if err != nil {
		t.Fatalf("embedded nagios: %v", err)
	}
	digest("nagios", nagios)

	got := strings.Join(lines, "\n") + "\n"
	golden := filepath.Join("testdata", "embedded-yaml.golden")
	if os.Getenv("UPDATE_YAML_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(want) != got {
		t.Fatalf("embedded YAML decodes differently:\n want:\n%s got:\n%s", want, got)
	}
}
