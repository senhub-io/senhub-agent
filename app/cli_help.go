package app

import (
	"fmt"
	"io"
)

// helpRequested reports whether a help flag appears before the argument
// terminator. Flags after "--" are operands, not options.
func helpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

// selfHelpCommands are commands whose own parser prints help and exits
// before performing any action.
var selfHelpCommands = map[string]struct{}{
	"license":      {},
	"update":       {},
	"install":      {},
	"uninstall":    {},
	"start":        {},
	"stop":         {},
	"restart":      {},
	"status":       {},
	"run":          {},
	"refresh-unit": {},
}

// interceptsHelp reports whether the shared CLI intercepts help for the
// command, as opposed to the command's own parser handling it.
func interceptsHelp(command string) bool {
	_, selfHelp := selfHelpCommands[command]
	return !selfHelp
}

// commandUsage returns the usage writer for a top-level command.
func commandUsage(command string) func(io.Writer) {
	switch command {
	case "secret":
		return writeSecretUsage
	case "key":
		return func(w io.Writer) {
			fmt.Fprintln(w, keyUsage)
		}
	case "zabbix":
		return func(w io.Writer) {
			fmt.Fprint(w, zabbixUsage)
		}
	}
	if c, ok := extraCommands[command]; ok && c.Usage != nil {
		return c.Usage
	}
	return func(io.Writer) {
		// showHelp prints to stdout.
		showHelp()
	}
}

// interceptHelp handles `<command> ... -h|--help` before dispatch. It
// returns true when it wrote usage and the command must not run.
func interceptHelp(args []string, out io.Writer) bool {
	if len(args) < 3 {
		return false
	}
	command := args[1]
	if !interceptsHelp(command) {
		return false
	}
	if !helpRequested(args[2:]) {
		return false
	}
	commandUsage(command)(out)
	return true
}
