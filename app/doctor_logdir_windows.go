//go:build windows

package app

import "errors"

func hostLogDirAccess(string, string) (logDirState, error) {
	return logDirState{}, errors.New("directory permissions are not read on Windows")
}
