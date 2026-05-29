package main

// Thin entrypoint for the open-source agent binary.
//
// All CLI dispatch and command handling lives in the importable
// senhub-agent.go/app package; this file exists only to provide the
// package main / func main() shell the Go toolchain needs to link a
// binary. The free-tier probe set is registered transitively by the
// app package (see app/probes_register.go).
//
// The companion enterprise repository ships its own package main that
// blank-imports the paid probe packages alongside app.Main(), reusing
// the exact same dispatch without forking it — that reuse is the whole
// reason the dispatch moved out of cmd/agent and into app.
import "senhub-agent.go/app"

func main() { app.Main() }
