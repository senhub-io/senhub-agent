//go:build windows

package auto_update

import "fmt"

// statOwnership has no Windows implementation: the ownership question is a
// Linux one (is the executable modifiable by the unprivileged service account),
// and the Windows branch of the preflight never calls this.
func statOwnership(path string) (ownership, error) {
	return ownership{}, fmt.Errorf("ownership is not evaluated on windows: %s", path)
}
