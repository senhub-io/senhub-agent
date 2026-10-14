//go:build !linux && !windows

package hostnet

import "errors"

func platformNeighbors() ([]hostNeighbor, error) {
	return nil, errors.New("no neighbour table reader on this platform")
}
