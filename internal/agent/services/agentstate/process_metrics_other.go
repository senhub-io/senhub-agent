//go:build !linux && !windows

package agentstate

// getResidentMemory / getOpenFDs are best-effort on platforms without
// a /proc filesystem and without the Win32 psapi APIs (macOS, *BSD,
// …). They return 0; the caller treats 0 as "unknown" and downstream
// dashboards display "No data" for those panels rather than misleading
// values.
//
// macOS specifically: the canonical sources are libproc
// (proc_pidinfo PROC_PIDTASKINFO) which requires CGO. We deliberately
// keep the agent CGO-free for cross-build simplicity; macOS deploys
// are dev-only today.

func getResidentMemory() uint64 { return 0 }
func getOpenFDs() int           { return 0 }
