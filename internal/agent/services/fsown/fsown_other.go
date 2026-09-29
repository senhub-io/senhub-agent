//go:build !linux

// Package fsown keeps the files a privileged command writes readable by the
// service that runs without privilege.
package fsown

// AlignToDir has nothing to do outside Linux: the Windows service runs as
// SYSTEM and macOS is a development target.
func AlignToDir(string) error { return nil }
