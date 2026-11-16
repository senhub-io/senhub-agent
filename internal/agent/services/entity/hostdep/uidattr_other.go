//go:build !linux

package hostdep

func buildUIDView() *uidView { return nil }
