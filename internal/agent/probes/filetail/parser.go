package filetail

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// ProbeType is the canonical type name used across the registry, the
// licence catalogue, and the LogRecord producer identity.
const ProbeType = "filetail"

// commonTimestampLayouts is the fallback set tried (in order) when a
// parser declares a TimestampField but no explicit TimestampFormat.
var commonTimestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	time.RFC1123Z,
	time.RFC1123,
}

// severityFromText maps a free-form level token (case-insensitive) to
// an OTel severity number + canonical text. Unknown tokens yield
// (Unspecified, "") so the caller can leave severity empty.
func severityFromText(level string) (agentstate.LogSeverity, string) {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "TRACE":
		return agentstate.LogSeverityTrace, "TRACE"
	case "DEBUG", "DBG":
		return agentstate.LogSeverityDebug, "DEBUG"
	case "INFO", "INFORMATION", "NOTICE":
		return agentstate.LogSeverityInfo, "INFO"
	case "WARN", "WARNING":
		return agentstate.LogSeverityWarn, "WARN"
	case "ERROR", "ERR":
		return agentstate.LogSeverityError, "ERROR"
	case "FATAL", "CRITICAL", "CRIT", "EMERG", "PANIC":
		return agentstate.LogSeverityFatal, "FATAL"
	default:
		return agentstate.LogSeverityUnspecified, ""
	}
}

// parseLine turns one assembled logical line into a LogRecord according
// to the parser config. readTime is the wall-clock instant the line was
// read; it is used as the record timestamp unless the parser extracts
// one from the content. probeName / file annotate the producer.
//
// Returns ok=false only for the JSON parser on a line that is not a
// JSON object — such a line is malformed for a declared jsonl source
// and the caller logs+skips it. Every other parser always produces a
// record (raw is the universal fallback).
func parseLine(pc ParserConfig, line string, readTime time.Time, probeName, file string) (agentstate.LogRecord, bool) {
	rec := agentstate.LogRecord{
		Timestamp:         readTime,
		Severity:          agentstate.LogSeverityUnspecified,
		Body:              line,
		Attributes:        map[string]string{},
		ProducerProbeName: probeName,
		ProducerProbeType: ProbeType,
	}
	if file != "" {
		rec.Attributes["log.file.path"] = file
	}

	switch pc.Type {
	case ParserRegex:
		applyRegex(pc, line, &rec)
	case ParserJSON:
		if !applyJSON(pc, line, &rec) {
			return rec, false
		}
	case ParserLogfmt:
		applyLogfmt(pc, line, &rec)
	case ParserRaw:
		// body already set
	}

	return rec, true
}

func applyRegex(pc ParserConfig, line string, rec *agentstate.LogRecord) {
	if pc.compiled == nil {
		return
	}
	m := pc.compiled.FindStringSubmatch(line)
	if m == nil {
		// No match: keep the raw line as body, no structured attrs.
		return
	}
	names := pc.compiled.SubexpNames()
	fields := map[string]string{}
	for i, name := range names {
		if i == 0 || name == "" || i >= len(m) {
			continue
		}
		fields[name] = m[i]
	}
	liftFields(pc, fields, rec)
}

func applyJSON(pc ParserConfig, line string, rec *agentstate.LogRecord) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || trimmed[0] != '{' {
		return false
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return false
	}
	fields := make(map[string]string, len(obj))
	for k, v := range obj {
		fields[k] = stringifyJSONValue(v)
	}
	liftFields(pc, fields, rec)
	return true
}

func applyLogfmt(pc ParserConfig, line string, rec *agentstate.LogRecord) {
	fields := parseLogfmt(line)
	if len(fields) == 0 {
		return
	}
	liftFields(pc, fields, rec)
}

// liftFields applies the common post-parse mapping shared by all
// structured parsers: every field becomes an attribute, a "message"/
// "msg"/"body" field promotes to the record body, a "level"/"severity"
// field sets the severity, and the configured timestamp field (if any
// and parseable) overrides the record timestamp.
func liftFields(pc ParserConfig, fields map[string]string, rec *agentstate.LogRecord) {
	for k, v := range fields {
		rec.Attributes[k] = v
	}

	if body, ok := firstNonEmpty(fields, "message", "msg", "body"); ok {
		rec.Body = body
	}
	if lvl, ok := firstNonEmpty(fields, "level", "severity", "lvl"); ok {
		if sev, text := severityFromText(lvl); sev != agentstate.LogSeverityUnspecified {
			rec.Severity = sev
			rec.SeverityText = text
		}
	}

	if pc.TimestampField != "" {
		if raw, ok := fields[pc.TimestampField]; ok && raw != "" {
			if ts, ok := parseTimestamp(raw, pc.TimestampFormat); ok {
				rec.Timestamp = ts
			}
		}
	}
}

// parseTimestamp parses raw using the explicit layout when provided,
// otherwise it tries a unix-epoch interpretation and then the common
// layout set. Returns ok=false when nothing parses.
func parseTimestamp(raw, layout string) (time.Time, bool) {
	if layout != "" {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
		return time.Time{}, false
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		// Heuristic: ms vs s by magnitude (> ~ year 2286 in seconds).
		if i > 1e12 {
			return time.UnixMilli(i), true
		}
		return time.Unix(i, 0), true
	}
	for _, l := range commonTimestampLayouts {
		if t, err := time.Parse(l, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func firstNonEmpty(m map[string]string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v, true
		}
	}
	return "", false
}

func stringifyJSONValue(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// json numbers decode to float64; render integers without a
		// trailing ".0" for clean attribute values.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
}

// parseLogfmt parses a single logfmt line into a flat map. It handles
// bare keys (treated as key=""), unquoted values, and double-quoted
// values with standard escapes. Malformed fragments are skipped rather
// than failing the whole line.
func parseLogfmt(line string) map[string]string {
	out := map[string]string{}
	i := 0
	n := len(line)
	for i < n {
		for i < n && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}
		keyStart := i
		for i < n && line[i] != '=' && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		key := line[keyStart:i]
		if key == "" {
			i++
			continue
		}
		if i >= n || line[i] != '=' {
			out[key] = ""
			continue
		}
		i++ // skip '='
		if i < n && line[i] == '"' {
			i++
			var sb strings.Builder
			for i < n && line[i] != '"' {
				if line[i] == '\\' && i+1 < n {
					i++
					switch line[i] {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					default:
						sb.WriteByte(line[i])
					}
				} else {
					sb.WriteByte(line[i])
				}
				i++
			}
			if i < n {
				i++ // closing quote
			}
			out[key] = sb.String()
		} else {
			valStart := i
			for i < n && line[i] != ' ' && line[i] != '\t' {
				i++
			}
			out[key] = line[valStart:i]
		}
	}
	return out
}
