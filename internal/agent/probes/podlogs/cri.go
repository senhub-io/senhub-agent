// Package podlogs provides pure, reusable building blocks for reading
// Kubernetes pod logs from a node's files. It parses CRI and Docker JSON log
// lines, reassembles partial chunks into whole records, and parses and filters
// pod log paths without starting probes or goroutines.
package podlogs

import (
	"encoding/json"
	"strings"
	"time"
)

// Entry is a parsed CRI or Docker JSON log chunk.
type Entry struct {
	Time      time.Time
	Stream    string
	Partial   bool
	Message   string
	Truncated bool
}

// ParseLine parses one physical log line from a CRI or Docker JSON log file.
// A malformed line returns ok=false.
func ParseLine(line string) (Entry, bool) {
	if strings.HasPrefix(line, "{") {
		return parseDockerJSON(line)
	}
	return parseCRILine(line)
}

func parseCRILine(line string) (Entry, bool) {
	tsStr, rest, ok := strings.Cut(line, " ")
	if !ok {
		return Entry{}, false
	}
	stream, rest, ok := strings.Cut(rest, " ")
	if !ok {
		return Entry{}, false
	}
	tag, msg, ok := strings.Cut(rest, " ")
	if !ok {
		return Entry{}, false
	}

	if stream != "stdout" && stream != "stderr" {
		return Entry{}, false
	}

	partial, ok := parseCRITag(tag)
	if !ok {
		return Entry{}, false
	}

	t, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		return Entry{}, false
	}

	return Entry{Time: t, Stream: stream, Partial: partial, Message: msg}, true
}

func parseCRITag(tag string) (partial bool, ok bool) {
	first, _, _ := strings.Cut(tag, ":")
	switch first {
	case "F":
		return false, true
	case "P":
		return true, true
	default:
		return false, false
	}
}

func parseDockerJSON(line string) (Entry, bool) {
	var raw struct {
		Log    *string `json:"log"`
		Stream *string `json:"stream"`
		Time   *string `json:"time"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Entry{}, false
	}
	if raw.Log == nil || raw.Stream == nil || raw.Time == nil {
		return Entry{}, false
	}
	if *raw.Stream != "stdout" && *raw.Stream != "stderr" {
		return Entry{}, false
	}

	t, err := time.Parse(time.RFC3339Nano, *raw.Time)
	if err != nil {
		return Entry{}, false
	}

	log := *raw.Log
	partial := !strings.HasSuffix(log, "\n")
	if !partial {
		log = strings.TrimSuffix(log, "\n")
	}

	return Entry{Time: t, Stream: *raw.Stream, Partial: partial, Message: log}, true
}
