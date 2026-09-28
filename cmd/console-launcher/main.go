// Command console-launcher opens the SenHub Agent web console without
// showing a terminal.
//
// The agent is a console program: Windows allocates it a console window
// before any of its own code runs, so a shortcut aimed straight at it
// shows one. It used to sit in front of the browser for as long as
// `senhub-agent console` waited for the service to answer — up to
// fifteen seconds on a fresh install, which is exactly when a person is
// watching.
//
// A shortcut pointing here shows nothing at all: this program is built
// for the Windows GUI subsystem (-H windowsgui), so no console is ever
// allocated. It starts the agent's console verb detached and exits.
//
// It replaces a VBScript launcher. VBScript is a feature-on-demand
// since Windows 11 24H2 and slated for removal; a scripting host
// launching a signed binary is a pattern endpoint protection flags; and
// the script was the one piece of executable content in the package the
// Authenticode chain did not cover. This is signed with everything
// else.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// agentName is the binary this opens the console of. It is looked up
// beside this launcher rather than on PATH: an installation carries
// both in the same directory, and PATH may hold another agent.
const agentName = "senhub-agent.exe"

func main() {
	self, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	agent := filepath.Join(filepath.Dir(self), agentName)
	if _, err := os.Stat(agent); err != nil {
		os.Exit(1)
	}

	// Pass through whatever the shortcut adds, so one launcher serves
	// the console verb and anything the installer wants to hand it.
	args := append([]string{"console"}, os.Args[1:]...)
	cmd := exec.Command(agent, args...) // #nosec G204 - a fixed name next to this binary
	cmd.Dir = filepath.Dir(self)

	// Start and leave: the agent handles its own elevation prompt and
	// opens the browser. Waiting would keep this process alive for the
	// whole fifteen-second endpoint wait for no reason.
	if err := cmd.Start(); err != nil {
		os.Exit(1)
	}
	_ = cmd.Process.Release()
}
