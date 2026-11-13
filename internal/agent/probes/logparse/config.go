// Package logparse turns raw log lines into OTel-shaped log records: the
// line parsers (raw, regex, json, logfmt), the multiline assembler that
// folds stack traces into one record, and the params blocks that
// configure them. It is shared by every conduit that reads lines from
// somewhere (a file, a remote stream) and publishes them on the log rail.
package logparse

import (
	"fmt"
	"regexp"
	"strings"
	"time"
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
// Pattern describes the line that STARTS a record when Match is "after"
// (the default): a matching line opens a new record and a non-matching
// line is a continuation of the record above it. With Match "before" a
// matching line ends the record: it is appended, then the record is sent.
// Negate inverts the test.
//
// This is not Filebeat's model, where the pattern describes the lines that
// are joined: Filebeat's "negate: true, match: after" for a leading
// timestamp is written here "negate: false". MultilineWarning flags the
// Filebeat spelling.
type MultilineConfig struct {
	Pattern  string
	compiled *regexp.Regexp
	Negate   bool
	Match    string // "after" (default) or "before"
	// FlushTimeout sends a record in progress once no line has come for
	// that long, so the last message of a quiet file is not held until
	// the next one. Zero keeps it until the next record starts. Set by a
	// conduit that streams lines (filetail); one that polls flushes on its
	// own idle cycle instead.
	FlushTimeout time.Duration
}

// DefaultMultilineFlushTimeout is how long a record in progress waits for
// another line when the probe sets no flush_timeout.
const DefaultMultilineFlushTimeout = 5 * time.Second

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

// MultilineWarning returns why a multiline block probably does not do what
// its author meant, or "" when nothing looks wrong. The one case it knows is
// Filebeat's spelling of "a line starting with this pattern opens a record"
// (negate: true, match: after, pattern anchored at the start of the line):
// here it makes every line WITHOUT the pattern open a record, so each line
// that has it is folded into the record a continuation line opened.
func MultilineWarning(ml MultilineConfig) string {
	if !ml.Negate || ml.Match != "after" || !strings.HasPrefix(ml.Pattern, "^") {
		return ""
	}
	return fmt.Sprintf("multiline: with negate: true and match: after, a line that does NOT match %q starts a record, and every line that matches is folded into it; "+
		"for a pattern on the first line of a message (Filebeat's negate: true), write negate: false", ml.Pattern)
}
