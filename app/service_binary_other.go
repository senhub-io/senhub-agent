//go:build !linux

package app

// Windows and darwin carry a single binary: the SCM ImagePath (or the
// launchd program) is the same file operators invoke, so there is no
// second copy to drift from. Windows upgrades additionally go through
// msiexec, which replaces the installed exe wholesale.

func installedServiceBinary() string { return "" }

func syncServiceBinary(string) (string, error) { return "", nil }
