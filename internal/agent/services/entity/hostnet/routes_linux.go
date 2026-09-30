//go:build linux

package hostnet

func platformRoutes() ([]hostRoute, error) { return procRoutes() }
