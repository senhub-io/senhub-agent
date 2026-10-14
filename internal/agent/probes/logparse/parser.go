package logparse

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

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
	case "TRACE", "TRC":
		return agentstate.LogSeverityTrace, "TRACE"
	case "DEBUG", "DBG":
		return agentstate.LogSeverityDebug, "DEBUG"
	case "INFO", "INFORMATION", "NOTICE", "NOTE", "SYSTEM", "INF":
		return agentstate.LogSeverityInfo, "INFO"
	case "WARN", "WARNING", "WRN":
		return agentstate.LogSeverityWarn, "WARN"
	case "ERROR", "ERR":
		return agentstate.LogSeverityError, "ERROR"
	case "FATAL", "CRITICAL", "CRIT", "CRT", "FTL", "EMERG", "PANIC":
		return agentstate.LogSeverityFatal, "FATAL"
	default:
		return agentstate.LogSeverityUnspecified, ""
	}
}

// ParseLine turns one assembled logical line into a LogRecord according
// to the parser config. readTime is the wall-clock instant the line was
// read; it is used as the record timestamp unless the parser extracts
// one from the content. probeName / probeType identify the producer.
//
// Returns ok=false only for the JSON parser on a line that is not a
// JSON object — such a line is malformed for a declared jsonl source
// and the caller logs+skips it. Every other parser always produces a
// record (raw is the universal fallback).
func ParseLine(pc ParserConfig, line string, readTime time.Time, probeName, probeType string) (agentstate.LogRecord, bool) {
	rec := agentstate.LogRecord{
		Timestamp:         readTime,
		Severity:          agentstate.LogSeverityUnspecified,
		Body:              line,
		Attributes:        map[string]string{},
		ProducerProbeName: probeName,
		ProducerProbeType: probeType,
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
	if rec.Severity == agentstate.LogSeverityUnspecified {
		if sev, text := SniffSeverity(line); sev != agentstate.LogSeverityUnspecified {
			rec.Severity, rec.SeverityText = sev, text
		}
	}

	return rec, true
}

// levelToken matches one level word in the first sniffHead bytes of a raw
// line. Named groups identify the form so the klog single-letter prefix
// can be mapped before severityFromText.
var levelToken = regexp.MustCompile(
	`^(?P<klog>[IWEF])[0-9]{4} [0-9]{1,2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?` +
		`|"(?:level|severity|lvl)"\s*:\s*"(?P<json>[^"]*)"` +
		`|\[[^\[\]:]+:(?P<apache>(?i:trace|debug|info|notice|warn|warning|error|err|fatal|critical|crit|panic|inf|wrn|dbg|trc|ftl|crt|severe|config|fine|finer|finest|log|note|system))\]` +
		`|\[(?P<bracket>(?i:trace|debug|info|notice|warn|warning|error|err|fatal|critical|crit|panic|inf|wrn|dbg|trc|ftl|crt|note|system))\]` +
		`|(?:^|\s)(?P<colon>SEVERE|FINE|FINER|FINEST|CONFIG|LOG):` +
		`|(?:^|\s)(?P<bare>TRACE|DEBUG|INFO|NOTICE|WARN|WARNING|ERROR|ERR|FATAL|CRITICAL|CRIT|PANIC|INF|WRN|DBG|TRC|FTL|CRT)(?:[\s:]|$)` +
		`|(?:^|\s)(?i:level|lvl|severity)="?(?P<logfmt>(?i:trace|debug|info|notice|warn|warning|error|err|fatal|critical|crit|panic|inf|wrn|dbg|trc|ftl|crt))(?:"|\s|$)` +
		`|^(?:\S+\s+){0,2}(?P<first>(?i:trace|debug|info|notice|warn|warning|error|err|fatal|critical|crit|panic))(?:[\s:]|$)`,
)

// accessLogShape matches the strict HTTP access-log shape. It is only
// run after levelToken finds no level word, so a stated level wins.
var accessLogShape = regexp.MustCompile(
	`^(?:\S+) (?:\S+) (?:\S+) \[[^\]]+\] "(?:\\.|[^"\\])*" ` +
		`(?P<status>[1-5]\d{2}) (?:\d+|-)(?: "(?:\\.|[^"\\])*" "(?:\\.|[^"\\])*")?(?: .*)?$`,
)

const sniffHead = 160

// SniffSeverity reads the level word at the head of a line that no
// structured parser handled, so a raw application log still carries a
// severity a consumer can filter and colour on. Unspecified when none.
func SniffSeverity(line string) (agentstate.LogSeverity, string) {
	head := line
	if len(head) > sniffHead {
		head = head[:sniffHead]
	}
	m := levelToken.FindStringSubmatch(head)
	if m == nil {
		return sniffAccessLogSeverity(line)
	}
	for i, name := range levelToken.SubexpNames() {
		if i == 0 || name == "" || m[i] == "" {
			continue
		}
		word := m[i]
		switch name {
		case "klog":
			switch word {
			case "I":
				word = "INFO"
			case "W":
				word = "WARN"
			case "E":
				word = "ERROR"
			case "F":
				word = "FATAL"
			}
		}
		if sev, text := severityFromText(word); sev != agentstate.LogSeverityUnspecified {
			return sev, text
		}
		// java.util.logging and PostgreSQL words stay out of the short
		// table, which also names the text a structured parser keeps.
		if sev := agentstate.SeverityFromText(word); sev != agentstate.LogSeverityUnspecified {
			return sev, word
		}
		return agentstate.LogSeverityUnspecified, ""
	}
	return agentstate.LogSeverityUnspecified, ""
}

// sniffAccessLogSeverity checks the HTTP access-log shape only after the
// level-word sniff found nothing. The cheap prefix check keeps the
// anchored regexp from running on every ordinary line.
func sniffAccessLogSeverity(line string) (agentstate.LogSeverity, string) {
	head := line
	if len(head) > 120 {
		head = head[:120]
	}
	lb := strings.Index(head, " [")
	rb := strings.Index(head, "] \"")
	if lb < 0 || rb < lb {
		return agentstate.LogSeverityUnspecified, ""
	}

	m := accessLogShape.FindStringSubmatch(line)
	if m == nil {
		return agentstate.LogSeverityUnspecified, ""
	}
	status := m[accessLogShape.SubexpIndex("status")]
	if status == "" {
		return agentstate.LogSeverityUnspecified, ""
	}

	switch status[0] {
	case '5':
		return agentstate.LogSeverityError, "ERROR"
	case '4':
		return agentstate.LogSeverityWarn, "WARN"
	case '1', '2', '3':
		return agentstate.LogSeverityInfo, "INFO"
	default:
		return agentstate.LogSeverityUnspecified, ""
	}
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
		} else if sev := agentstate.SeverityFromText(lvl); sev != agentstate.LogSeverityUnspecified {
			// A spelling outside the short table: the number is derived,
			// the text stays what the source wrote.
			rec.Severity = sev
			rec.SeverityText = strings.TrimSpace(lvl)
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
