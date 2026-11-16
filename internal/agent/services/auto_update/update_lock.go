package auto_update

import (
	"errors"
	"fmt"
	"path/filepath"
)

const updateLockFileName = "update.lock"

var ErrUpdateInProgress = errors.New("another update is in progress on this host")

func AcquireUpdateLock(stateDir string) (release func(), err error) {
	release, err = tryLockFile(filepath.Join(stateDir, updateLockFileName))
	if err != nil {
		if errors.Is(err, ErrUpdateInProgress) {
			return nil, err
		}
		return nil, fmt.Errorf("acquire update lock %s: %w", filepath.Join(stateDir, updateLockFileName), err)
	}
	return release, nil
}
