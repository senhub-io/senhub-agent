package agent

import (
	"errors"
	"fmt"

	"senhub-agent.go/internal/agent/services/auto_update"
	"senhub-agent.go/internal/agent/services/logger"
)

func takeInstallLock(acquire func() (func(), error), log *logger.Logger) (release func(), err error) {
	release, err = acquire()
	if err != nil {
		if errors.Is(err, auto_update.ErrUpdateInProgress) {
			return nil, fmt.Errorf("%w; wait for it to finish, then try again", err)
		}
		log.Warn().Err(err).Msg("Could not take the update lock; updating without it")
		return func() {}, nil
	}
	return release, nil
}
