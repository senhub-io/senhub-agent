package app

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

type updateWatchArgs struct {
	binary   string
	version  string
	from     string
	stateDir string
}

func parseUpdateWatchArgs(args []string) (updateWatchArgs, error) {
	fs := flag.NewFlagSet("update-watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var a updateWatchArgs
	fs.StringVar(&a.binary, "binary", "", "target binary path")
	fs.StringVar(&a.version, "version", "", "version that failed to start")
	fs.StringVar(&a.from, "from", "", "version being restored")
	fs.StringVar(&a.stateDir, "state-dir", "", "agent state directory")
	if err := fs.Parse(args); err != nil {
		return a, fmt.Errorf("parse update-watch args: %w", err)
	}
	var missing []string
	if a.binary == "" {
		missing = append(missing, "--binary")
	}
	if a.version == "" {
		missing = append(missing, "--version")
	}
	if a.from == "" {
		missing = append(missing, "--from")
	}
	if a.stateDir == "" {
		missing = append(missing, "--state-dir")
	}
	if len(missing) > 0 {
		return a, fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	return a, nil
}
