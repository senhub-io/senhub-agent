package strategyreg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store"
)

// mapKeyLiteral matches a string-literal map lookup, which is how every
// strategy reads its free-form configuration.
var mapKeyLiteral = regexp.MustCompile(`\["([a-z][a-z_0-9]*)"\]`)

// TestKnownParamsCoverEveryKeyTheStrategiesRead keeps the declarations
// honest.
//
// The declared set is what `agent config check` measures an operator's
// file against: a key outside it is reported as read by nobody. A new
// parameter added to a strategy without being declared here would
// therefore be reported as an error in a configuration that works —
// worse than the silence #846 removed. This test fails first.
func TestKnownParamsCoverEveryKeyTheStrategiesRead(t *testing.T) {
	for _, name := range data_store.StrategiesDeclaringParams() {
		declared := map[string]struct{}{}
		for _, key := range data_store.KnownParamsFor(name) {
			declared[key] = struct{}{}
		}

		dir := filepath.Join("..", "strategies", name)
		read := keysReadUnder(t, dir)
		if len(read) == 0 {
			t.Errorf("strategy %q: no source read under %s — the guard is not looking at the code", name, dir)
			continue
		}

		var missing []string
		for key := range read {
			if _, ok := declared[key]; !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			t.Errorf("strategy %q reads %s but does not declare them; add them to RegisterKnownParams or config check will reject a working file",
				name, strings.Join(missing, ", "))
		}
	}
}

func keysReadUnder(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	keys := map[string]struct{}{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path) // #nosec G304 - test walking the repository's own source
		if readErr != nil {
			return readErr
		}
		for _, m := range mapKeyLiteral.FindAllStringSubmatch(string(src), -1) {
			keys[m[1]] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return keys
}

// TestInsecureIsReportedWithItsReplacement pins the case that opened
// #846: the spelling other OTLP tooling uses, silently ignored while
// the agent negotiated TLS.
func TestInsecureIsReportedWithItsReplacement(t *testing.T) {
	unread := data_store.UnreadParamsFor("otlp", map[string]interface{}{
		"endpoint": "127.0.0.1:4317",
		"insecure": true,
	})

	if len(unread) != 1 {
		t.Fatalf("want the one key the strategy does not read, got %+v", unread)
	}
	if unread[0].Key != "insecure" {
		t.Errorf("reported %q, want \"insecure\"", unread[0].Key)
	}
	if !strings.Contains(unread[0].Replacement, "tls") {
		t.Errorf("the report must name the current spelling, got %q", unread[0].Replacement)
	}
}
