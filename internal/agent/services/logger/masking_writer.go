package logger

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logredact"
)

// MaskingWriter is an io.Writer that masks sensitive information before writing
type MaskingWriter struct {
	out io.Writer
}

// NewMaskingWriter creates a new writer that masks sensitive data
func NewMaskingWriter(out io.Writer) *MaskingWriter {
	return &MaskingWriter{out: out}
}

// Write implements the io.Writer interface.
//
// zerolog calls Write with one log entry per call, the payload
// already terminated by '\n'. json.Unmarshal silently ignores the
// trailing newline; json.Marshal does NOT emit one. Without a fix,
// successive entries concatenate into one giant physical line in the
// file, breaking `tail -f`, `Get-Content -Tail`, jq, and every
// line-oriented log aggregator (Loki / fluent-bit / Splunk forwarder).
// Always append '\n' to whatever shape we forward downstream — we
// preserve NDJSON whether the input was JSON, masked-as-string, or
// fell back to raw masking.
func (w *MaskingWriter) Write(p []byte) (n int, err error) {
	if _, writeErr := w.out.Write(w.mask(p)); writeErr != nil {
		return 0, writeErr
	}
	return len(p), nil // Return original input length
}

// WriteLevel masks the record once, then hands it to the wrapped writer
// with its level when that writer is level-aware (zerolog's MultiLevelWriter,
// the self-log rail), so a single MaskingWriter in front of the fan-out
// covers every output: console, file, shipper and OTLP alike.
func (w *MaskingWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	masked := w.mask(p)
	var writeErr error
	if lw, ok := w.out.(zerolog.LevelWriter); ok {
		_, writeErr = lw.WriteLevel(level, masked)
	} else {
		_, writeErr = w.out.Write(masked)
	}
	if writeErr != nil {
		return 0, writeErr
	}
	return len(p), nil
}

// mask returns the record with its sensitive values masked, always ending
// with exactly one newline: a JSON record is masked field by field and by
// value, anything else as text.
func (w *MaskingWriter) mask(p []byte) []byte {
	var jsonObj map[string]interface{}
	if json.Unmarshal(p, &jsonObj) == nil {
		masked := redactJSONStrings(maskJSONFields(jsonObj))
		if maskedJSON, err := json.Marshal(masked); err == nil {
			return appendNewlineIfMissing(maskedJSON)
		}
	}
	return appendNewlineIfMissing([]byte(redactText(MaskSensitiveData(string(p)))))
}

// redactProbe is the probe label of the agent's own log masking in
// senhub.agent.logs.redacted.
const redactProbe = "agent"

func countRedaction(rule string) { agentstate.RecordLogRedaction(redactProbe, rule) }

func redactText(s string) string { return logredact.Redact(s, countRedaction) }

// maskJSONFields recursively traverses a JSON object and masks sensitive fields
func maskJSONFields(obj map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range obj {
		newKey := key

		// Mask sensitive fields based on field name
		if isSensitiveFieldName(key) {
			switch v := value.(type) {
			case string:
				result[newKey] = maskValue(v)
				continue
			}
		}

		// Recursive processing of nested structures
		switch v := value.(type) {
		case map[string]interface{}:
			result[newKey] = maskJSONFields(v)
		case []interface{}:
			newArray := make([]interface{}, len(v))
			for i, item := range v {
				if mapItem, ok := item.(map[string]interface{}); ok {
					newArray[i] = maskJSONFields(mapItem)
				} else {
					newArray[i] = item
				}
			}
			result[newKey] = newArray
		default:
			result[newKey] = value
		}
	}

	return result
}

// redactJSONStrings applies the full credential redaction to every string
// value in a decoded JSON record, recursing through maps and arrays. Field
// names are left untouched; only values are rewritten.
func redactJSONStrings(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			t[k] = redactJSONStrings(vv)
		}
		return t
	case []interface{}:
		for i, vv := range t {
			t[i] = redactJSONStrings(vv)
		}
		return t
	case string:
		return redactText(t)
	default:
		return v
	}
}

// isSensitiveFieldName checks if a field name is likely to contain sensitive data
func isSensitiveFieldName(fieldName string) bool {
	sensitiveNames := []string{
		"password", "passwd", "pwd",
		"token", "api_key", "apikey",
		"secret", "credential",
		"authentication_key", "authkey",
		"authorization", "auth",
		// passphrase covers SNMPv3 priv_passphrase, which carries no
		// other keyword (auth_passphrase only matched by luck on
		// "auth"); community is the SNMP v2c shared secret.
		"passphrase", "community",
	}

	fieldNameLower := strings.ToLower(fieldName)
	for _, name := range sensitiveNames {
		if strings.Contains(fieldNameLower, name) {
			return true
		}
	}

	return false
}

// appendNewlineIfMissing ensures the buffer ends with exactly one '\n'.
// Cheap (one byte check + at most one append) — fine on the hot
// log-write path. Idempotent: returns the input unchanged when it
// already terminates with a newline.
func appendNewlineIfMissing(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		return b
	}
	return append(b, '\n')
}
