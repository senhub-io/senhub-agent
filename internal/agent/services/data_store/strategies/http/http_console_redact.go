package http

import (
	"regexp"
	"strings"

	"senhub-agent.go/internal/agent/services/configuration"
)

// The console needs a different view of a params map than a log line:
// a ${secret:...} or ${env:...} reference is what the file holds and is
// safe to show (it is how the form knows a secret is stored), and an
// identifier such as a user name is something the operator has to be
// able to read and edit. Only the values themselves are hidden.

// A superset of the keys the boot-time sealer treats as sensitive
// (secret.IsSensitiveKey), so nothing sealed is ever shown resolved.
var consoleSecretKeyPattern = regexp.MustCompile(`(?i)(password|passphrase|secret|token|api[_-]?key|private[_-]?key|credential|community|dsn|uri|authorization|bearer|license|jwt)`)

const redactedForConsole = "***"

// sanitizeForConsole returns a deep copy of params where a string value
// under a schema secret path, or under a key that looks like a secret,
// is replaced by "***" unless it is a ${...} reference. Everything else
// is returned as written.
func sanitizeForConsole(params map[string]interface{}, secretPaths []string) map[string]interface{} {
	if params == nil {
		return nil
	}
	secret := map[string]bool{}
	for _, p := range secretPaths {
		secret[p] = true
	}
	var walk func(prefix string, m map[string]interface{}, inSecret bool) map[string]interface{}
	walk = func(prefix string, m map[string]interface{}, inSecret bool) map[string]interface{} {
		out := make(map[string]interface{}, len(m))
		for k, v := range m {
			path := prefix + k
			hide := inSecret || secret[path] || consoleSecretKeyPattern.MatchString(k)
			switch val := v.(type) {
			case map[string]interface{}:
				out[k] = walk(path+".", val, hide)
			case map[interface{}]interface{}:
				conv := make(map[string]interface{}, len(val))
				for kk, vv := range val {
					if ks, ok := kk.(string); ok {
						conv[ks] = vv
					}
				}
				out[k] = walk(path+".", conv, hide)
			case []interface{}:
				items := make([]interface{}, len(val))
				for i, item := range val {
					if im, ok := item.(map[string]interface{}); ok {
						items[i] = walk(path+".", im, hide)
					} else if s, ok := item.(string); ok && hide && !strings.HasPrefix(s, "${") {
						items[i] = redactedForConsole
					} else {
						items[i] = item
					}
				}
				out[k] = items
			case string:
				if hide && val != "" && !strings.HasPrefix(val, "${") {
					out[k] = redactedForConsole
				} else {
					out[k] = val
				}
			default:
				out[k] = v
			}
		}
		return out
	}
	return walk("", params, false)
}

// dropRedactedValues removes, in place, every string leaf that is the
// console's own placeholder for a hidden value. A page that echoes the
// listing back must never turn "***" into a stored credential.
func dropRedactedValues(params map[string]interface{}) {
	for k, v := range params {
		switch val := v.(type) {
		case map[string]interface{}:
			dropRedactedValues(val)
			if len(val) == 0 {
				delete(params, k)
			}
		case []interface{}:
			for _, item := range val {
				if im, ok := item.(map[string]interface{}); ok {
					dropRedactedValues(im)
				}
			}
		case string:
			if val == redactedForConsole || val == "[REDACTED]" {
				delete(params, k)
			}
		}
	}
}

// paramsAsWritten is what the fragment will hold once the values the
// form left out are taken back from the file: the checks must run on
// that, not on what the form re-sent, or a stored required secret would
// read as missing on every edit.
func paramsAsWritten(stored, incoming map[string]interface{}) map[string]interface{} {
	out := configuration.KeepStoredReferences(stored, copyParams(incoming))
	configuration.DropNilValues(out)
	return out
}

// copyParams deep-copies params, nulls included: a null is the console
// saying "drop the stored value", and the check must see it.
func copyParams(params map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(params))
	for k, v := range params {
		out[k] = copyParamValue(v)
	}
	return out
}

func copyParamValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return copyParams(val)
	case []interface{}:
		list := make([]interface{}, len(val))
		for i, item := range val {
			list[i] = copyParamValue(item)
		}
		return list
	default:
		return v
	}
}

// withoutNils returns a deep copy of params with every nil leaf and every
// mapping emptied by it removed: what a shape check should see when a
// form sends null to remove a stored entry.
func withoutNils(params map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(params))
	for k, v := range params {
		switch val := v.(type) {
		case nil:
		case map[string]interface{}:
			if sub := withoutNils(val); len(sub) > 0 {
				out[k] = sub
			}
		default:
			out[k] = v
		}
	}
	return out
}
