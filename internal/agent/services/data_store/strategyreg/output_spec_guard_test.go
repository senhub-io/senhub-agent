package strategyreg

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

// TestOutputSpecsDeclareOnlyKeysTheStrategiesRead keeps the console's
// output schemas within what the code reads: a key offered in a form
// that no strategy reads would be written to a file and ignored, which
// is the silence #846 removed for hand-written files.
func TestOutputSpecsDeclareOnlyKeysTheStrategiesRead(t *testing.T) {
	registered := map[string]bool{}
	for _, name := range data_store.StrategiesDeclaringParams() {
		registered[name] = true
	}
	for _, o := range outputspec.Registered() {
		if !registered[o.Type] {
			t.Errorf("output %q declares a schema but no known params; register both", o.Type)
			continue
		}
		known := map[string]struct{}{}
		for _, key := range data_store.KnownParamsFor(o.Type) {
			known[key] = struct{}{}
		}
		var unread []string
		for path := range o.DeclaredKeys() {
			for _, seg := range strings.Split(path, ".") {
				if _, ok := known[seg]; !ok && !outputGuardAllowed(o.Type, seg) {
					unread = append(unread, path)
					break
				}
			}
		}
		if len(unread) > 0 {
			t.Errorf("output %q declares %v but the strategy does not read them", o.Type, unread)
		}
		if o.DisplayName == "" || o.Mode == "" {
			t.Errorf("output %q needs a DisplayName and a Mode", o.Type)
		}
	}
	for _, name := range []string{"otlp", "http", "prtg", "senhub", "event"} {
		if _, ok := outputspec.For(name); !ok {
			t.Errorf("strategy %q ships without an output schema", name)
		}
	}
}

// outputGuardAllowed lists declared keys the static scan of the known
// params cannot see. Every entry needs a reason.
var outputGuardAllowList = map[string]map[string]string{}

func outputGuardAllowed(output, key string) bool {
	_, ok := outputGuardAllowList[output][key]
	return ok
}
