//go:build !linux

package configuration

// existingOwner is Linux-only: Windows protects the folder by ACL and
// darwin is a development target.
func existingOwner(string) *fileOwner { return nil }
