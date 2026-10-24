package app

import (
	"bytes"
	"fmt"
	"os"
)

// unitRefresher is what refreshing the unit after update needs from the
// host, so the decision can be tested without systemd.
type unitRefresher struct {
	unitPath func() string
	read     func(path string) ([]byte, error)
	// run runs refresh-unit --yes from the new binary, so the unit comes
	// from the template of the version just installed, not the one that
	// downloaded it.
	run func(newBinary string) ([]byte, error)
}

// refresh brings a CLI-installed unit up to date with the new binary. A
// host with no unit, or whose unit belongs to the .deb/.rpm package, has
// nothing to do here. It returns a line to print when the unit changed.
func (r unitRefresher) refresh(newBinary string) (string, error) {
	path := r.unitPath()
	before, err := r.read(path)
	if err != nil || unitOwnedByPackage(path) {
		return "", nil
	}
	if out, err := r.run(newBinary); err != nil {
		return "", fmt.Errorf("%s refresh-unit: %w: %s", newBinary, err, bytes.TrimSpace(out))
	}
	after, err := r.read(path)
	if err != nil {
		return "", fmt.Errorf("reading %s after refresh-unit: %w", path, err)
	}
	if bytes.Equal(before, after) {
		return "", nil
	}
	return fmt.Sprintf("Systemd unit refreshed from the new binary: %s", path), nil
}

// writeRefreshedUnit replaces the unit file and reloads systemd. It writes
// that one file: drop-ins under <unit>.d/ are the operator's and stay as
// they are, so systemd applies them to the refreshed unit after the reload.
func writeRefreshedUnit(unitPath, refreshed string, reload func() error) error {
	if err := os.WriteFile(unitPath, []byte(refreshed), 0o644); err != nil { // #nosec G306 - systemd units are world-readable
		return fmt.Errorf("writing unit file: %w", err)
	}
	return reload()
}
