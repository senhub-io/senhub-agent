//go:build !linux && !windows

package process

import "errors"

func loggedInSessions() (float64, error) {
	return 0, errors.New("open login sessions are not counted on this OS")
}
