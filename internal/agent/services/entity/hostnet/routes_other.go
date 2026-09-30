//go:build !linux && !windows

package hostnet

import "errors"

func platformRoutes() ([]hostRoute, error) {
	return nil, errors.New("no routing table reader on this platform")
}
