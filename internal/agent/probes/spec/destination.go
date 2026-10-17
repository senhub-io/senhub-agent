package spec

import "regexp"

var destinationKey = regexp.MustCompile(`(?i)^(hosts?|hostnames?|urls?|uris?|endpoints?|address(es)?|addr|servers?|targets?|dsn|brokers|nodes|socket(_path)?|connection_string|.*_(url|uri|host|hosts|endpoint|address|addr|server|servers))$`)

// DestinationPaths lists the dotted paths of the parameters that say where
// the agent connects: the ones a credential must not follow when they
// change. A port alone is not listed: the same host owner receives the
// credential, and the http output and listeners carry ports that bind rather
// than reach. Schemas do not mark them, so they are recognised by name, at any
// depth, secrets excluded.
func DestinationPaths(params []ParamSpec) []string {
	var out []string
	var walk func(prefix string, ps []ParamSpec)
	walk = func(prefix string, ps []ParamSpec) {
		for _, p := range ps {
			if !p.Secret && p.Kind != KindBlock && p.Kind != KindBlockList && destinationKey.MatchString(p.Key) {
				out = append(out, prefix+p.Key)
			}
			walk(prefix+p.Key+".", p.Fields)
		}
	}
	walk("", params)
	return out
}
