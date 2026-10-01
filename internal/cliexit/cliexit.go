// Package cliexit defines the process exit codes of the agent's command
// line. Scripts and orchestration tools branch on them, so they live in
// one place and are never written as bare numbers at the call site.
package cliexit

const (
	// OK means the command did what was asked.
	OK = 0
	// Warning means the command ran to completion but something needs
	// attention: the result is usable, not clean.
	Warning = 1
	// Failure means the command could not do what was asked, including
	// a malformed invocation.
	Failure = 2
	// Unchanged means the system already was in the requested state and
	// nothing was written.
	Unchanged = 3
)

// Name returns the stable lowercase label of an exit code, as printed in
// the JSON output of the commands that support --json.
func Name(code int) string {
	switch code {
	case OK:
		return "ok"
	case Warning:
		return "warning"
	case Failure:
		return "failure"
	case Unchanged:
		return "unchanged"
	default:
		return "unknown"
	}
}
