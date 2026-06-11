package execprobe

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// checkMetric is one parsed measurement from a check's output. The
// final metric name is always namespaced under senhub.exec.* so an
// operator script can never collide with an internal metric.
type checkMetric struct {
	name     string
	value    float64
	otelType string // "gauge" or "counter"
	tags     map[string]string
}

// metricName builds the namespaced, sanitized final name.
func metricName(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	trimmed := strings.Trim(b.String(), "_.")
	if trimmed == "" {
		// Labels like "/" (check_disk's root filesystem) sanitize to
		// nothing; keep the measurement under a stable fallback name.
		trimmed = "perfdata"
	}
	return "senhub.exec." + trimmed
}

// parseNagiosPerfdata extracts the perfdata section ("TEXT | perfdata")
// and parses each 'label'=value[UOM][;warn[;crit[;min[;max]]]] token.
// Time units are normalized to seconds and byte units to bytes; a "c"
// UOM marks a counter, per the plugin development guidelines.
// Malformed tokens are skipped — one bad token must not void a check.
func parseNagiosPerfdata(stdout []byte) []checkMetric {
	out := string(stdout)
	idx := strings.IndexByte(out, '|')
	if idx < 0 {
		return nil
	}
	perf := strings.TrimSpace(out[idx+1:])
	// Long plugin output may hold a second "text | perfdata" block;
	// keeping everything after the first pipe and splitting on
	// whitespace tolerates both layouts because labels are re-anchored
	// on the '=' sign.
	var metrics []checkMetric
	for _, token := range splitPerfTokens(perf) {
		m, ok := parsePerfToken(token)
		if ok {
			metrics = append(metrics, m)
		}
	}
	return metrics
}

// splitPerfTokens splits perfdata on whitespace while keeping quoted
// labels ('rta max'=0.5ms) together.
func splitPerfTokens(perf string) []string {
	var tokens []string
	var b strings.Builder
	inQuote := false
	for _, r := range perf {
		switch {
		case r == '\'':
			inQuote = !inQuote
			b.WriteRune(r)
		case (r == ' ' || r == '\n' || r == '\t' || r == '\r') && !inQuote:
			if b.Len() > 0 {
				tokens = append(tokens, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		tokens = append(tokens, b.String())
	}
	return tokens
}

func parsePerfToken(token string) (checkMetric, bool) {
	var label, rest string
	if strings.HasPrefix(token, "'") {
		end := strings.Index(token[1:], "'")
		if end < 0 {
			return checkMetric{}, false
		}
		label = token[1 : end+1]
		rest = token[end+2:]
	} else {
		eq := strings.IndexByte(token, '=')
		if eq < 0 {
			return checkMetric{}, false
		}
		label = token[:eq]
		rest = token[eq:]
	}
	if label == "" || !strings.HasPrefix(rest, "=") {
		return checkMetric{}, false
	}
	// value[UOM] is the first ';'-field after '='.
	valueField := rest[1:]
	if semi := strings.IndexByte(valueField, ';'); semi >= 0 {
		valueField = valueField[:semi]
	}
	numEnd := len(valueField)
	for i, r := range valueField {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' && r != 'e' && r != 'E' {
			numEnd = i
			break
		}
	}
	value, err := strconv.ParseFloat(valueField[:numEnd], 64)
	if err != nil {
		return checkMetric{}, false
	}
	uom := valueField[numEnd:]

	m := checkMetric{name: metricName(label), value: value, otelType: "gauge"}
	switch strings.ToLower(uom) {
	case "", "%":
		// dimensionless / percent: value as-is
	case "s":
	case "ms":
		m.value *= 1e-3
	case "us":
		m.value *= 1e-6
	case "b":
	case "kb":
		m.value *= 1024
	case "mb":
		m.value *= 1024 * 1024
	case "gb":
		m.value *= 1024 * 1024 * 1024
	case "tb":
		m.value *= 1024 * 1024 * 1024 * 1024
	case "c":
		m.otelType = "counter"
	default:
		// Unknown UOM: keep the raw value rather than guessing a scale.
	}
	return m, true
}

// jsonOutput is the exec probe's JSON contract.
type jsonOutput struct {
	Status  *int `json:"status"`
	Metrics []struct {
		Name  string            `json:"name"`
		Value float64           `json:"value"`
		Type  string            `json:"type"`
		Tags  map[string]string `json:"tags"`
	} `json:"metrics"`
}

// parseJSONOutput parses the JSON contract. A missing status falls
// back to the Nagios exit-code mapping so scripts can rely on either.
func parseJSONOutput(stdout []byte, exitCode int) (int, []checkMetric, error) {
	var out jsonOutput
	if err := json.Unmarshal(stdout, &out); err != nil {
		return 3, nil, fmt.Errorf("parsing exec JSON output: %w", err)
	}

	status := nagiosStatus(exitCode, false)
	if out.Status != nil {
		if *out.Status < 0 || *out.Status > 3 {
			return 3, nil, fmt.Errorf("exec JSON status must be 0..3 (got %d)", *out.Status)
		}
		status = *out.Status
	}

	metrics := make([]checkMetric, 0, len(out.Metrics))
	for _, m := range out.Metrics {
		if m.Name == "" {
			continue
		}
		otelType := "gauge"
		if m.Type == "counter" {
			otelType = "counter"
		}
		metrics = append(metrics, checkMetric{
			name:     metricName(m.Name),
			value:    m.Value,
			otelType: otelType,
			tags:     m.Tags,
		})
	}
	return status, metrics, nil
}

// buildEnv extends the agent's environment with the configured extras.
// The parent environment is inherited deliberately: checks routinely
// need PATH, HOME or locale, and the agent does not hold secrets in
// its own environment.
func buildEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
