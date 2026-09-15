// Package logparse turns raw log lines into OTel-shaped log records: the
// line parsers (raw, regex, json, logfmt), the multiline assembler that
// folds stack traces into one record, and the params blocks that
// configure them. It is shared by every conduit that reads lines from
// somewhere (a file, a remote stream) and publishes them on the log rail.
package logparse

import (
	"fmt"
	"regexp"
)

// ParserType enumerates the supported line-parsing strategies. Each
// value maps to a concrete parser in parser.go.
type ParserType string

const (
	// ParserRaw ships the whole line as the log body, no structured
	// extraction. The safe default for opaque application logs.
	ParserRaw ParserType = "raw"
	// ParserRegex extracts named capture groups from each line via a
	// configured regular expression.
	ParserRegex ParserType = "regex"
	// ParserJSON decodes each line as a JSON object (jsonl) and lifts
	// every key into a log attribute.
	ParserJSON ParserType = "json"
	// ParserLogfmt decodes each line as logfmt (key=value pairs).
	ParserLogfmt ParserType = "logfmt"
)

// DefaultMaxBytesPerLine caps the length of a single assembled log
// record (after multiline folding). Lines longer than this are
// truncated to protect against OOM on monstrous stacktraces / JSON
// blobs, per the issue's max_bytes_per_line note.
const DefaultMaxBytesPerLine = 1 << 20 // 1 MiB

// MultilineConfig describes how consecutive physical lines fold into a
// single logical log record. When Pattern is empty multiline is off
// and every physical line is its own record.
//
// Semantics mirror the common filebeat/fluentbit model:
//   - A line matching Pattern (XOR Negate) is a continuation that is
//     appended to the record currently being assembled when Match is
//     "after"; when Match is "before" the matching line starts a new
//     record and the preceding accumulation is flushed.
//
// The default match mode is "after": Pattern marks the FIRST line of a
// new message (e.g. a leading timestamp), so a non-matching line is a
// continuation of the message above it.
type MultilineConfig struct {
	Pattern  string
	compiled *regexp.Regexp
	Negate   bool
	Match    string // "after" (default) or "before"
}

// Enabled reports whether multiline folding is active (a pattern compiled).
func (m MultilineConfig) Enabled() bool { return m.compiled != nil }

// ParserConfig captures the line-to-attributes mapping.
type ParserConfig struct {
	Type     ParserType
	Pattern  string
	compiled *regexp.Regexp

	// TimestampField names the parsed field (regex capture group or
	// JSON/logfmt key) that carries the record timestamp. Empty means
	// "use the time the line was read".
	TimestampField string
	// TimestampFormat is the Go reference-time layout used to parse
	// TimestampField. Empty falls back to a small set of common
	// layouts (RFC3339, etc.).
	TimestampFormat string
}

// Compiled reports whether the regex parser has a usable pattern.
func (p ParserConfig) Compiled() bool { return p.compiled != nil }

// ParseMultiline reads the optional `multiline` block of a probe's params
// into ml. A missing block leaves ml untouched.
func ParseMultiline(config map[string]interface{}, ml *MultilineConfig) error {
	raw, ok := config["multiline"].(map[string]interface{})
	if !ok {
		return nil
	}
	ml.Match = "after"
	if s, ok := raw["pattern"].(string); ok {
		ml.Pattern = s
	}
	if v, ok := raw["negate"].(bool); ok {
		ml.Negate = v
	}
	if s, ok := raw["match"].(string); ok && s != "" {
		if s != "after" && s != "before" {
			return fmt.Errorf("multiline.match must be \"after\" or \"before\", got %q", s)
		}
		ml.Match = s
	}
	if ml.Pattern != "" {
		re, err := regexp.Compile(ml.Pattern)
		if err != nil {
			return fmt.Errorf("compiling multiline.pattern %q: %w", ml.Pattern, err)
		}
		ml.compiled = re
	}
	return nil
}

// ParseParser reads the optional `parser` block of a probe's params into
// pc. A missing block leaves pc untouched; a regex parser without a usable
// pattern is an error, an operator mistake worth surfacing at construction.
func ParseParser(config map[string]interface{}, pc *ParserConfig) error {
	raw, ok := config["parser"].(map[string]interface{})
	if !ok {
		return nil
	}

	if s, ok := raw["type"].(string); ok && s != "" {
		switch ParserType(s) {
		case ParserRaw, ParserRegex, ParserJSON, ParserLogfmt:
			pc.Type = ParserType(s)
		default:
			return fmt.Errorf("unknown parser.type %q (want raw|regex|json|logfmt)", s)
		}
	}
	if s, ok := raw["pattern"].(string); ok {
		pc.Pattern = s
	}
	if s, ok := raw["timestamp_field"].(string); ok {
		pc.TimestampField = s
	}
	if s, ok := raw["timestamp_format"].(string); ok {
		pc.TimestampFormat = s
	}

	if pc.Type == ParserRegex {
		if pc.Pattern == "" {
			return fmt.Errorf("parser.type=regex requires a non-empty parser.pattern")
		}
		re, err := regexp.Compile(pc.Pattern)
		if err != nil {
			return fmt.Errorf("compiling parser.pattern %q: %w", pc.Pattern, err)
		}
		if countNamedGroups(re.SubexpNames()) == 0 {
			return fmt.Errorf("parser.pattern %q has no named capture groups", pc.Pattern)
		}
		pc.compiled = re
	}
	return nil
}

// countNamedGroups returns how many of a regexp's subexpression names
// are non-empty. regexp.SubexpNames() always includes the whole-match
// entry at index 0 (always "") plus one entry per capture group; an
// unnamed group also yields "". Only non-empty names are usable by the
// regex parser, so a pattern like `^(.+)$` (one unnamed group) must be
// rejected as having no named groups.
func countNamedGroups(names []string) int {
	n := 0
	for _, name := range names {
		if name != "" {
			n++
		}
	}
	return n
}
