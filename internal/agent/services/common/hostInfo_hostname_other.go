//go:build !windows

package common

// resolveHostFQDN has no dedicated source on non-Windows platforms: the
// OS-reported hostname (often already an FQDN on servers) stands as-is,
// so only the lower-case normalization in canonicalHostname applies.
func resolveHostFQDN() string { return "" }
