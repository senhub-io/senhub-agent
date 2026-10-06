// Package state tells a probe where the agent keeps what must survive a
// restart: log bookmarks, read offsets, cursors. A probe that persists
// something and has no path configured by the operator uses Path as its
// default, so the file lands beside the agent's identity instead of in the
// working directory, which a service does not control.
package state

import (
	"path/filepath"

	"senhub-agent.go/internal/agent/services/instanceid"
)

// EnvDir overrides the state directory. The container image sets it with
// the same name its entrypoint reads.
const EnvDir = "SENHUB_STATE_DIR"

// Dir is the agent's state directory: SENHUB_STATE_DIR when set, else the
// directory the service manager hands the unit (STATE_DIRECTORY), else the
// platform default (/var/lib/senhub-agent, C:\ProgramData\SenHub).
func Dir() string {
	return instanceid.OwnStateDir()
}

// Path is the file name inside the state directory. The name must be
// distinct per probe instance (a bookmark shared by two instances makes
// each replay or skip what the other read), so build it from the instance
// name: state.Path(name + ".bookmark").
func Path(name string) string {
	return filepath.Join(Dir(), filepath.Base(name))
}
