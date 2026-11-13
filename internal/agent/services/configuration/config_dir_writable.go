package configuration

import (
	"os"
	"sync"
)

// ConfigDirWritable reports whether dir accepts file creation right now
// (EROFS, EACCES and a missing directory all report false). It is checked
// per call, never cached: a mount can change between two loads.
func ConfigDirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".senhub-write-check-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

var warnOnceSeen sync.Map

// warnOnce runs fn the first time the cause is seen for dir in this process.
// Keys combine the cause and directory, so a condition that recurs on every
// configuration load is reported once per directory.
func warnOnce(cause, dir string, fn func()) {
	key := cause + "\x00" + dir
	if _, loaded := warnOnceSeen.LoadOrStore(key, struct{}{}); !loaded {
		fn()
	}
}

// resetWarnOnceForTest clears the warn-once state.
func resetWarnOnceForTest() {
	warnOnceSeen.Range(func(key, _ any) bool {
		warnOnceSeen.Delete(key)
		return true
	})
}
