package template

import (
	"regexp"
	"strings"
)

var lldMacro = regexp.MustCompile(`\{#[A-Z0-9_.]+\}`)

// nameRegexp matches the names a prototype's items take once the server
// has resolved its macros. An override operation is matched against that
// resolved name, not against the prototype's own: measured on 7.0.30, an
// EQUAL on "P {#IF} a" matched nothing, a regular expression "^P .+ a$"
// matched "P ens3 a".
func nameRegexp(name string) string {
	var b strings.Builder
	b.WriteByte('^')
	last := 0
	for _, loc := range lldMacro.FindAllStringIndex(name, -1) {
		b.WriteString(regexp.QuoteMeta(name[last:loc[0]]))
		b.WriteString(".+")
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(name[last:]))
	b.WriteByte('$')
	return b.String()
}
