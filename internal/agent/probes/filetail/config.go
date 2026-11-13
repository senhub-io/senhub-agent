package filetail

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/logparse"
	"senhub-agent.go/internal/agent/probes/types"
)

// The parser and multiline vocabulary lives in logparse, shared with every
// conduit that reads lines; these names keep filetail's own surface stable.
type (
	ParserType      = logparse.ParserType
	ParserConfig    = logparse.ParserConfig
	MultilineConfig = logparse.MultilineConfig
)

const (
	ParserRaw    = logparse.ParserRaw
	ParserRegex  = logparse.ParserRegex
	ParserJSON   = logparse.ParserJSON
	ParserLogfmt = logparse.ParserLogfmt

	DefaultMaxBytesPerLine = logparse.DefaultMaxBytesPerLine
)

// ProbeType is the canonical type name used across the registry, the
// licence catalogue, and the LogRecord producer identity.
const ProbeType = "filetail"

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

	parsed.Paths, _ = types.StringSliceParam(config, "paths")
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

	if err := parseMultilineFlushTimeout(config, &parsed.Multiline); err != nil {
		return parsed, fmt.Errorf("filetail: %w", err)
	}
	if err := logparse.ParseMultiline(config, &parsed.Multiline); err != nil {
		return parsed, fmt.Errorf("filetail: %w", err)
	}
	if err := logparse.ParseParser(config, &parsed.Parser); err != nil {
		return parsed, fmt.Errorf("filetail: %w", err)
	}

	return parsed, nil
}

// parseMultilineFlushTimeout reads multiline.flush_timeout into ml: a
// duration ("30s") or a number of seconds, 0 turning the timeout off, and
// logparse.DefaultMultilineFlushTimeout when the key is absent.
func parseMultilineFlushTimeout(config map[string]interface{}, ml *MultilineConfig) error {
	ml.FlushTimeout = logparse.DefaultMultilineFlushTimeout
	raw, ok := config["multiline"].(map[string]interface{})
	if !ok {
		return nil
	}
	v, present := raw["flush_timeout"]
	if !present {
		return nil
	}
	d, err := parseFlushTimeout(v)
	if err != nil {
		return err
	}
	ml.FlushTimeout = d
	return nil
}

func parseFlushTimeout(v interface{}) (time.Duration, error) {
	var d time.Duration
	switch t := v.(type) {
	case string:
		parsed, err := time.ParseDuration(strings.TrimSpace(t))
		if err != nil {
			secs, convErr := strconv.ParseFloat(strings.TrimSpace(t), 64)
			if convErr != nil {
				return 0, fmt.Errorf("multiline.flush_timeout must be a duration (\"30s\") or a number of seconds, got %q", t)
			}
			parsed = time.Duration(secs * float64(time.Second))
		}
		d = parsed
	case int:
		d = time.Duration(t) * time.Second
	case int64:
		d = time.Duration(t) * time.Second
	case float64:
		d = time.Duration(t * float64(time.Second))
	default:
		return 0, fmt.Errorf("multiline.flush_timeout must be a duration (\"30s\") or a number of seconds, got %v", v)
	}
	if d < 0 {
		return 0, fmt.Errorf("multiline.flush_timeout must not be negative, got %s", d)
	}
	return d, nil
}
