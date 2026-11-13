package auto_update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// keepPreviousBinary copies the executable about to be replaced to
// <binary>.previous, replacing the copy of an earlier update. It is a copy,
// not a link: the swap renames the original, and the copy must survive the
// removal of whatever the swap leaves behind. The copy is built under a
// temporary name so an interrupted write never leaves a truncated .previous.
func (e rollbackEnv) keepPreviousBinary() error {
	src, err := os.Open(e.binary) // #nosec G304 - the executable being replaced
	if err != nil {
		return fmt.Errorf("opening %s: %w", e.binary, err)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("reading the mode of %s: %w", e.binary, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(e.binary), ".senhub-agent-previous-*")
	if err != nil {
		return fmt.Errorf("creating the previous-binary copy next to %s: %w", e.binary, err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("copying %s: %w", e.binary, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("flushing the copy of %s: %w", e.binary, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing the copy of %s: %w", e.binary, err)
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("setting the mode of the copy of %s: %w", e.binary, err)
	}
	if err := os.Rename(tmpName, e.previousPath()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("publishing %s: %w", e.previousPath(), err)
	}
	return nil
}
