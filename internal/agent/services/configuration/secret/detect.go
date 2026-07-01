package secret

import (
	"regexp"
	"strconv"
	"strings"
)

// sensitiveKeyPattern matches config field NAMES that carry a secret value and
// therefore must be sealed. It is deliberately narrower than the log-redaction
// pattern: identifier-style fields (`user`, `login`, `email`) are NOT secrets
// and must not be moved into the store. Connection strings (`dsn`, `uri`) are
// included because they embed a password.
var sensitiveKeyPattern = regexp.MustCompile(`(?i)(password|passphrase|secret|token|api[_-]?key|community|credential|dsn|uri|private[_-]?key)`)

// InlineSecret is a plaintext secret discovered in a configuration field.
type InlineSecret struct {
	InstanceName string   // owning probe / strategy name
	FieldPath    []string // e.g. ["password"], ["director","auth","password"], ["v3","users","0","auth_password"]
	Value        string   // the plaintext — handle with care, never log
}

// Key returns the instance-qualified, backend-safe store key for this secret
// (e.g. "veeam-prod.password").
func (s InlineSecret) Key() string {
	return SanitizeKey(s.InstanceName + "." + strings.Join(s.FieldPath, "."))
}

// Ref returns the reference string that replaces the inline value in the config
// (e.g. "${secret:veeam-prod.password}").
func (s InlineSecret) Ref() string {
	return "${secret:" + s.Key() + "}"
}

// IsSensitiveKey reports whether a field name denotes a secret to seal.
func IsSensitiveKey(name string) bool { return sensitiveKeyPattern.MatchString(name) }

// FindInlineSecrets walks one instance's params and returns every field whose
// NAME denotes a secret and whose VALUE is a plaintext (not already a ${...}
// reference and not empty). It recurses into nested maps and indexed slices, so
// citrix `director.auth.password` and snmp `v3.users[0].auth_password` are found.
func FindInlineSecrets(instanceName string, params map[string]interface{}) []InlineSecret {
	var out []InlineSecret
	walkSecrets(instanceName, nil, params, &out)
	return out
}

func walkSecrets(instance string, path []string, node interface{}, out *[]InlineSecret) {
	switch n := node.(type) {
	case map[string]interface{}:
		for k, v := range n {
			child := append(append([]string(nil), path...), k)
			if s, ok := v.(string); ok {
				if IsSensitiveKey(k) && isPlaintextSecret(s) {
					*out = append(*out, InlineSecret{InstanceName: instance, FieldPath: child, Value: s})
				}
				continue
			}
			walkSecrets(instance, child, v, out)
		}
	case map[interface{}]interface{}:
		// yaml.v2 produces this map shape for nested objects.
		for rk, v := range n {
			k, ok := rk.(string)
			if !ok {
				continue
			}
			child := append(append([]string(nil), path...), k)
			if s, ok := v.(string); ok {
				if IsSensitiveKey(k) && isPlaintextSecret(s) {
					*out = append(*out, InlineSecret{InstanceName: instance, FieldPath: child, Value: s})
				}
				continue
			}
			walkSecrets(instance, child, v, out)
		}
	case []interface{}:
		for i, v := range n {
			child := append(append([]string(nil), path...), strconv.Itoa(i))
			walkSecrets(instance, child, v, out)
		}
	}
}

// isPlaintextSecret reports whether a string value is a real inline secret
// rather than an already-resolved reference (${env:}/${file:}/${secret:}) or an
// empty value.
func isPlaintextSecret(v string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	return !strings.HasPrefix(strings.TrimSpace(v), "${")
}
