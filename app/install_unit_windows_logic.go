package app

import "strings"

// serviceCommandLine builds the ImagePath the SCM should hold for exe
// and args; escape renders one argument the way Windows does.
func serviceCommandLine(exe string, args []string, escape func(string) string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, escape(exe))
	for _, a := range args {
		parts = append(parts, escape(a))
	}
	return strings.Join(parts, " ")
}

// serviceCommandLineMatches compares a registered ImagePath with the
// wanted one: same executable and the same argument text after it.
func serviceCommandLineMatches(registered, exe string, args []string, escape func(string) string) bool {
	regExe, rest, ok := splitImagePath(strings.TrimSpace(registered))
	if !ok {
		return false
	}
	regExe = strings.Trim(regExe, `"`)
	if !samePath(regExe, exe) {
		return false
	}
	var want string
	if len(args) > 0 {
		escaped := make([]string, 0, len(args))
		for _, a := range args {
			escaped = append(escaped, escape(a))
		}
		want = strings.Join(escaped, " ")
	}
	return strings.TrimSpace(rest) == strings.TrimSpace(want)
}
