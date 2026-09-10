package docsparams

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
)

func TestRenderPutsANestedKeyUnderItsDottedPath(t *testing.T) {
	p := spec.Probe{Type: "x", Params: []spec.ParamSpec{
		{Key: "mode", Kind: spec.KindString, Default: "local", Enum: []string{"local", "remote"}, Description: "Where to read"},
		{Key: "remote", Kind: spec.KindBlock, Description: "Remote access", Fields: []spec.ParamSpec{
			{Key: "host", Kind: spec.KindString, Required: true},
			{Key: "password", Kind: spec.KindString, Secret: true},
		}},
		{Key: "users", Kind: spec.KindBlockList, Fields: []spec.ParamSpec{
			{Key: "username", Kind: spec.KindString},
		}},
	}}
	out := Render(p)
	for _, want := range []string{
		"| `mode` | No | `local` |",
		"One of `local`, `remote`",
		"| `remote.host` | Yes | - |",
		"| `remote.password` | No | - |",
		"A secret: reference it with",
		"| `users[].username` | No | - |",
		Start, End,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the table must contain %q:\n%s", want, out)
		}
	}
}

func TestRenderSaysSoWhenAProbeReadsNothing(t *testing.T) {
	out := Render(spec.Probe{Type: "y"})
	if !strings.Contains(out, "reads no parameters") {
		t.Errorf("a probe with no parameter must say so, got:\n%s", out)
	}
}

// A description holding a pipe would otherwise break the table.
func TestRenderEscapesAPipe(t *testing.T) {
	out := Render(spec.Probe{Type: "z", Params: []spec.ParamSpec{
		{Key: "k", Kind: spec.KindString, Description: "a | b"},
	}})
	if !strings.Contains(out, `a \| b`) {
		t.Errorf("a pipe must be escaped, got:\n%s", out)
	}
}
