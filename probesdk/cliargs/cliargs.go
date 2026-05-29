// Package cliArgs is the public mirror of the agent's parsed CLI args
// type (senhub-agent.go/internal/agent/cliArgs). Probe tests reference
// ParsedArgs to construct probes the way the runtime does.
package cliArgs

import icliArgs "senhub-agent.go/internal/agent/cliArgs"

// ParsedArgs carries the resolved command-line arguments handed to a
// probe at construction time.
type ParsedArgs = icliArgs.ParsedArgs
