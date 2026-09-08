// Package outputspec declares what the console needs to know about an
// output (a sync strategy) that its constructor cannot tell it: a
// display name, whether it pulls or pushes, and the parameters it
// accepts, in the same ParamSpec shape the probe schemas use, so one
// form engine renders both.
package outputspec

import (
	"fmt"
	"sort"
	"sync"

	"senhub-agent.go/internal/agent/probes/spec"
)

// Mode says which way the data moves.
type Mode string

const (
	// ModePull outputs serve data to a poller that comes and reads it.
	ModePull Mode = "pull"
	// ModePush outputs deliver data to a remote endpoint on their own.
	ModePush Mode = "push"
)

// Output is one strategy type's declaration.
type Output struct {
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	Summary     string `json:"summary,omitempty"`
	Mode        Mode   `json:"mode"`
	// Singleton outputs exist at most once and are never added from the
	// console: the HTTP strategy is created by the install and the
	// console itself runs on it.
	Singleton bool             `json:"singleton,omitempty"`
	DocsPath  string           `json:"docs_path,omitempty"`
	Params    []spec.ParamSpec `json:"params"`
}

var (
	mu    sync.RWMutex
	specs = map[string]Output{}
)

// Register declares an output's schema. Called from the strategy
// package's init(), next to the parser it describes. A duplicate or
// nameless registration is a programming error.
func Register(o Output) {
	if o.Type == "" {
		panic("outputspec: Register with an empty Type")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := specs[o.Type]; dup {
		panic(fmt.Sprintf("outputspec: %q registered twice", o.Type))
	}
	specs[o.Type] = o
}

// For returns the schema of an output type, if one is declared.
func For(outputType string) (Output, bool) {
	mu.RLock()
	defer mu.RUnlock()
	o, ok := specs[outputType]
	return o, ok
}

// Registered returns every declared schema, sorted by type.
func Registered() []Output {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Output, 0, len(specs))
	for _, o := range specs {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// CheckParams validates a params map against the schema, with the same
// rules as a probe schema.
func (o Output) CheckParams(params map[string]interface{}) []spec.SpecProblem {
	return spec.Probe{Type: o.Type, Params: o.Params}.CheckParams(params)
}

// DeclaredKeys flattens the declared keys as "block.field" paths.
func (o Output) DeclaredKeys() map[string]struct{} {
	return spec.Probe{Type: o.Type, Params: o.Params}.DeclaredKeys()
}

// SecretPaths lists the dotted parameter paths the schema marks secret;
// a secret mapping seals every value it holds.
func (o Output) SecretPaths() []string {
	var out []string
	var walk func(prefix string, params []spec.ParamSpec)
	walk = func(prefix string, params []spec.ParamSpec) {
		for _, p := range params {
			if p.Secret {
				out = append(out, prefix+p.Key)
			}
			if p.Kind == spec.KindBlock {
				walk(prefix+p.Key+".", p.Fields)
			}
		}
	}
	walk("", o.Params)
	return out
}
