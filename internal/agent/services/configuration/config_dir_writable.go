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

// warnOnce runs fn the first time key is seen in this process. Keys are
// short strings naming the cause, so a condition that recurs on every
// configuration load is reported once.
func warnOnce(key string, fn func()) {
	if _, loaded := warnOnceSeen.LoadOrStore(key, struct{}{}); !loaded {
		fn()
	}
}
