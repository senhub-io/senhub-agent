package auto_update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"senhub-agent.go/internal/agent/services/logger"
)

// RemoveUpdateLeftovers cleans up after a previous update of the running
// executable. It runs at agent start whether or not auto-update is enabled,
// since a manual "update" leaves the same files.
func RemoveUpdateLeftovers(base *logger.Logger) {
	target, err := os.Executable()
	if err != nil {
		return
	}
	log := logger.NewModuleLogger(base, "service.auto_update")
	for _, path := range removeUpdateLeftovers(target) {
		log.Info().Str("path", path).Msg("removed a file left by the previous update")
	}
}

// removeUpdateLeftovers deletes the files a binary replacement leaves next
// to the executable: the previous binary set aside as ".<name>.old" and a
// download that never completed, ".<name>.new". On Windows the running
// executable cannot be deleted, so the replacement only hides the old file;
// it is removable once the process that ran it has exited, which is by the
// time the next one starts. Left alone it keeps the install folder from
// being removed by an uninstall. It returns the files it removed.
func removeUpdateLeftovers(target string) []string {
	dir, name := filepath.Split(target)
	var removed []string
	for _, leftover := range []string{"." + name + ".old", "." + name + ".new"} {
		path := filepath.Join(dir, leftover)
		err := os.Remove(path)
		switch {
		case err == nil:
			removed = append(removed, path)
		case errors.Is(err, fs.ErrNotExist):
		default:
			// Still held by a process that has not exited; the next start retries.
		}
	}
	return removed
}
