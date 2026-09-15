package probes_test

import (
	"os"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/docsparams"
)

// docsDir is where the user guide's probe pages live, relative to this
// package.
const docsDir = "../../../docs/user-guide/docs/probes"

// TestProbePagesCarryTheirSchema keeps every page's parameter table equal
// to the schema its parser is guarded against. Run with UPDATE_DOCS=1 to
// rewrite the generated blocks (make docs-params).
//
// Only the probe types registered in this build are checked. The pages of
// the probes that live in senhub-agent-enterprise are held to the same
// rule by the same code, from that module, where their schemas exist:
// see probes/specguard/docs_params_test.go there.
func TestProbePagesCarryTheirSchema(t *testing.T) {
	res, err := docsparams.Sync(docsDir, spec.Registered(), os.Getenv("UPDATE_DOCS") == "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NoPage) > 0 {
		t.Errorf("%d probe types have no documentation page: %s", len(res.NoPage), strings.Join(res.NoPage, ", "))
	}
	if len(res.Stale) > 0 {
		t.Errorf("%d pages no longer match their schema; run make docs-params:\n  %s",
			len(res.Stale), strings.Join(res.Stale, "\n  "))
	}
}
