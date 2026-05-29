package app

import "fmt"

// ExtraCommand is a top-level subcommand contributed from outside the
// core dispatch. It exists so a build can add a verb to the shared CLI
// without the core importing the package that implements it — the
// enterprise build registers its `ibmi` command this way, keeping the
// IBM i runtime bridge (and its bundled jt400.jar) out of the
// open-source binary entirely.
//
// This mirrors the probe self-registration pattern (probes.RegisterProbe):
// the command package calls RegisterCommand from an init(), and the
// entrypoint blank-imports it.
type ExtraCommand struct {
	// Name is the os.Args[1] verb that triggers this command.
	Name string
	// ReadOnly bypasses the privilege gate. Set it only for pure
	// diagnostics that read state and print to stdout without touching
	// the service, network, log dir, or binary.
	ReadOnly bool
	// Run executes the command. Like the built-in handlers it reads
	// os.Args itself, so it can parse its own sub-verbs and flags.
	Run func()
}

// extraCommands holds the registered top-level subcommands, keyed by
// verb. Populated by RegisterCommand from init() callbacks.
var extraCommands = map[string]ExtraCommand{}

// RegisterCommand wires a top-level subcommand into the shared CLI.
// Intended to be called from an init() in the command's own package:
//
//	func init() {
//	    app.RegisterCommand(app.ExtraCommand{Name: "ibmi", Run: handleIBMICommand})
//	}
//
// Panics on a programmer error — empty name, nil Run, a name that
// shadows a built-in verb, or a duplicate registration. Detection at
// init time is the point: an ambiguous CLI must fail before Main runs.
func RegisterCommand(c ExtraCommand) {
	if c.Name == "" {
		panic("app.RegisterCommand: empty name")
	}
	if c.Run == nil {
		panic(fmt.Sprintf("app.RegisterCommand(%q): nil Run", c.Name))
	}
	if _, builtin := knownTopLevelArgs[c.Name]; builtin {
		panic(fmt.Sprintf("app.RegisterCommand(%q): shadows a built-in command or flag", c.Name))
	}
	if _, exists := extraCommands[c.Name]; exists {
		panic(fmt.Sprintf("app.RegisterCommand(%q): duplicate registration", c.Name))
	}
	extraCommands[c.Name] = c
}
