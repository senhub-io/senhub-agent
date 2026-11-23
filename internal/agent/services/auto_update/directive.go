package auto_update

import "sync/atomic"

// Directive is the update setting a server pushes. While one is in force it
// replaces auto_update.enabled and auto_update.version of the local file;
// the registry URL and include_beta stay local.
type Directive struct {
	// Auto turns the automatic update on or off.
	Auto bool
	// TargetVersion is an exact X.Y.Z or X.Y.Z-beta.N; empty means the
	// latest published stable version.
	TargetVersion string
	// AllowDowngrade permits a target lower than the running version.
	AllowDowngrade bool
	// Retry permits a target that failed to start on this agent, once per
	// Revision.
	Retry bool
	// Revision is the revision of the server setting the directive comes
	// from. A directive applied again at the same revision (a restart, a
	// resend) does not retry a second time.
	Revision uint64
}

var (
	directive     atomic.Pointer[Directive]
	directiveHook atomic.Pointer[func()]
)

// SetDirective puts a directive in force, or removes it with nil so the local
// file rules again. When the effective setting changes, an update check runs
// at once, without waiting for the next scheduled one.
func SetDirective(d *Directive) {
	var next *Directive
	if d != nil {
		c := *d
		next = &c
	}
	prev := directive.Swap(next)
	if sameDirective(prev, next) {
		return
	}
	if hook := directiveHook.Load(); hook != nil {
		go (*hook)()
	}
}

// CurrentDirective returns a copy of the directive in force, nil when none.
func CurrentDirective() *Directive {
	d := directive.Load()
	if d == nil {
		return nil
	}
	c := *d
	return &c
}

func sameDirective(a, b *Directive) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func setDirectiveHook(f func()) {
	if f == nil {
		directiveHook.Store(nil)
		return
	}
	directiveHook.Store(&f)
}
