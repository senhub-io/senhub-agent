package auto_update

import "github.com/hashicorp/go-version"

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
