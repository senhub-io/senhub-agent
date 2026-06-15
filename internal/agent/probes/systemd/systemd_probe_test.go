//go:build linux

// Linux-only tests that require a live D-Bus connection or linux-specific
// probe behaviour. Pure helper tests live in helpers_test.go (no build tag).
package systemd
