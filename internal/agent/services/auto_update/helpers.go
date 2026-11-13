package auto_update

import (
	"fmt"

	"github.com/hashicorp/go-version"
)

// errFirst returns the first non-nil error from the arguments, or nil
// when all are nil. Useful when logging a parse-failure that may
// stem from either side of a comparison.
func errFirst(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// shouldUpdateTo reports whether the agent at currentStr should apply
// the update offered as expectedStr. The rule is strict-greater-than
// per semver, with pre-release ordering honoured: "0.1.91" never
// supplants "0.1.94-beta" (downgrade), but "0.1.94" does (release
// outranks pre-release of the same triplet).
//
// Fail-closed on parse errors: if either side is unparseable the
// function returns (false, err) — the alternative (proceed on
// unparseable input) is exactly what triggered the production
// downgrade incident on a Windows production host.
func shouldUpdateTo(currentStr, expectedStr string) (bool, error) {
	cur, errCur := version.NewVersion(currentStr)
	exp, errExp := version.NewVersion(expectedStr)
	if errCur != nil || errExp != nil {
		return false, errFirst(errCur, errExp)
	}
	return exp.GreaterThan(cur), nil
}

// decideUpdate applies the update policy for one candidate version.
// operatorExplicit means the operator named a concrete version on the
// command line; such a request may go backwards, except on an
// MSI-managed install, where Windows Installer forbids downgrades in
// place. For non-operator callers it is exactly shouldUpdateTo.
func decideUpdate(currentStr, expectedStr string, operatorExplicit, msiManaged bool) (proceed, downgrade bool, err error) {
	cur, errCur := version.NewVersion(currentStr)
	exp, errExp := version.NewVersion(expectedStr)
	if errCur != nil || errExp != nil {
		err := errFirst(errCur, errExp)
		if operatorExplicit {
			return false, false, fmt.Errorf("cannot compare versions %q and %q: %w", currentStr, expectedStr, err)
		}
		return false, false, err
	}
	if !operatorExplicit {
		return exp.GreaterThan(cur), false, nil
	}
	switch {
	case exp.GreaterThan(cur):
		return true, false, nil
	case cur.GreaterThan(exp):
		if msiManaged {
			return false, true, fmt.Errorf("%s is older than the installed %s; an MSI install cannot be downgraded in place: uninstall it, then install the %s MSI", expectedStr, currentStr, expectedStr)
		}
		return true, true, nil
	default:
		return false, false, nil
	}
}

// IsExactVersion reports whether s names one release rather than an
// alias ("latest") or a constraint (">=0.6"): only such a request may
// go backwards.
func IsExactVersion(s string) bool {
	_, err := version.NewVersion(s)
	return err == nil
}
