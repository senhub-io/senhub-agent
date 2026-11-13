//go:build !windows

package auto_update

// Off Windows the service never replaces its own binary, so there is
// nothing for an operator update to race with.
func tryLockFile(string) (func(), error) {
	return func() {}, nil
}
