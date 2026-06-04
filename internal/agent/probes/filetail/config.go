package filetail

import (
	"fmt"
	"regexp"

	"senhub-agent.go/internal/agent/probes/types"
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

// FileTailProbeConfig is the fully-parsed, validated operator config.
type FileTailProbeConfig struct {
	// Paths is the set of files or globs to tail. Globs are expanded
	// and re-evaluated as files rotate / appear.
	Paths []string

	Multiline MultilineConfig
	Parser    ParserConfig

	// BookmarkPath is the JSON file persisting per-file read offsets so
	// a restart resumes without loss or duplication. Empty disables
	// persistence (the probe then tails from end-of-file on start).
	BookmarkPath string

	// MaxBytesPerLine caps a single logical record's size. 0 means use
	// DefaultMaxBytesPerLine.
	MaxBytesPerLine int

	// FromBeginning reads existing file content from offset 0 on first
	// sight of a file (when no bookmark exists). Default false: tail
	// from the end, only new lines.
	FromBeginning bool
}

// parseConfig converts the free-form YAML map into a validated config.
// It is intentionally strict on the parser block (a bad regex is an
// operator error worth surfacing at construction) and permissive on
// everything else.
func parseConfig(config map[string]interface{}) (FileTailProbeConfig, error) {
	parsed := FileTailProbeConfig{
		Parser:          ParserConfig{Type: ParserRaw},
		MaxBytesPerLine: DefaultMaxBytesPerLine,
	}

	parsed.Paths = stringSlice(config["paths"])
	if len(parsed.Paths) == 0 {
		return parsed, fmt.Errorf("filetail: at least one entry under `paths` is required")
	}

	if s, ok := config["bookmark_path"].(string); ok {
		parsed.BookmarkPath = s
	}
	if v, ok := config["from_beginning"].(bool); ok {
		parsed.FromBeginning = v
	}
	if n, ok := types.IntParam(config, "max_bytes_per_line"); ok {
		if n <= 0 {
			return parsed, fmt.Errorf("filetail: max_bytes_per_line must be > 0, got %d", n)
		}
		parsed.MaxBytesPerLine = n
	}

	if err := parseMultiline(config, &parsed.Multiline); err != nil {
		return parsed, err
	}
	if err := parseParser(config, &parsed.Parser); err != nil {
		return parsed, err
	}

	return parsed, nil
}

func parseMultiline(config map[string]interface{}, ml *MultilineConfig) error {
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
			return fmt.Errorf("filetail: multiline.match must be \"after\" or \"before\", got %q", s)
		}
		ml.Match = s
	}
	if ml.Pattern != "" {
		re, err := regexp.Compile(ml.Pattern)
		if err != nil {
			return fmt.Errorf("filetail: compiling multiline.pattern %q: %w", ml.Pattern, err)
		}
		ml.compiled = re
	}
	return nil
}

func parseParser(config map[string]interface{}, pc *ParserConfig) error {
	raw, ok := config["parser"].(map[string]interface{})
	if !ok {
		return nil
	}

	if s, ok := raw["type"].(string); ok && s != "" {
		switch ParserType(s) {
		case ParserRaw, ParserRegex, ParserJSON, ParserLogfmt:
			pc.Type = ParserType(s)
		default:
			return fmt.Errorf("filetail: unknown parser.type %q (want raw|regex|json|logfmt)", s)
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
			return fmt.Errorf("filetail: parser.type=regex requires a non-empty parser.pattern")
		}
		re, err := regexp.Compile(pc.Pattern)
		if err != nil {
			return fmt.Errorf("filetail: compiling parser.pattern %q: %w", pc.Pattern, err)
		}
		if countNamedGroups(re.SubexpNames()) == 0 {
			return fmt.Errorf("filetail: parser.pattern %q has no named capture groups", pc.Pattern)
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

// stringSlice normalises a YAML/JSON list-of-strings (or a lone string)
// into a []string, dropping empties. yaml.v2 decodes a list into
// []interface{}; a single scalar arrives as a bare string.
func stringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}
