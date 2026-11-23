package auto_update

import "sync/atomic"

// PrivilegedInstaller is the way an install that cannot replace itself
// gets an update: the unprivileged agent asks, something with the rights
// installs. It is the seam a package-based install plugs into; with none
// registered, such an install reports not_replaceable.
type PrivilegedInstaller interface {
	// Available reports whether the privileged path exists on this host
	// right now.
	Available() bool
	// Request asks for the installation of version, a published X.Y.Z or
	// X.Y.Z-beta.N, and returns once the request is recorded.
	Request(version string) error
}

var privilegedInstaller atomic.Pointer[PrivilegedInstaller]

// RegisterPrivilegedInstaller sets the privileged path; nil removes it.
func RegisterPrivilegedInstaller(p PrivilegedInstaller) {
	if p == nil {
		privilegedInstaller.Store(nil)
		return
	}
	privilegedInstaller.Store(&p)
}

func registeredPrivilegedInstaller() PrivilegedInstaller {
	if p := privilegedInstaller.Load(); p != nil && (*p).Available() {
		return *p
	}
	return nil
}

// canReplaceSelf says whether an install of this kind can put a new version
// in place from the running agent: an MSI hands over to msiexec, a Windows or
// other non-Linux install rewrites its own binary, and on Linux the service
// does not (see selfApplyRefusalFor) unless a privileged path is registered.
func canReplaceSelf(goos, kind string) bool {
	if kind == KindMSI || goos != "linux" {
		return true
	}
	return registeredPrivilegedInstaller() != nil
}
