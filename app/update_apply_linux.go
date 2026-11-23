//go:build linux

package app

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/auto_update"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/cliexit"
)

// runUpdateApply is the body of senhub-agent-update.service: it installs the
// version the service asked for, after auditing the request. It takes no
// argument, so nothing the caller controls can steer it, and it logs to
// standard error, which the journal captures.
func runUpdateApply(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "update-apply takes no argument: the request is read from the update request directory")
		return 2
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "update-apply must run as root")
		return 2
	}
	base := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true, TimeFormat: time.RFC3339}).With().Timestamp().Logger()
	log := logger.NewModuleLogger(&base, "service.root_update")
	if err := auto_update.RootApply(auto_update.RootApplyOptions{
		Logger:  log,
		Restart: restartServiceAfterRootUpdate,
	}); err != nil {
		return cliexit.Failure
	}
	return cliexit.OK
}
