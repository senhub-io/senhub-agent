package linuxlogs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// journalEntry models the subset of the journalctl JSON payload we
// care about. journalctl emits ALL fields for matched records — many
// are private ('_' prefix) or noisy (binary blobs). We pick the ones
// useful for an OTel log record and keep everything else under the
// "extras" map for downstream operators who want them.
//
// The JSON shape is documented at
// https://systemd.io/JOURNAL_EXPORT_FORMATS/
//
// Most numeric fields arrive as JSON strings (journalctl quotes
// every value). We parse the ones we need.
type journalEntry struct {
	Priority         string `json:"PRIORITY"`
	Message          string `json:"MESSAGE"`
	Hostname         string `json:"_HOSTNAME"`
	SystemdUnit      string `json:"_SYSTEMD_UNIT"`
	SyslogIdentifier string `json:"SYSLOG_IDENTIFIER"`
	PID              string `json:"_PID"`
	UID              string `json:"_UID"`
	Comm             string `json:"_COMM"`
	Transport        string `json:"_TRANSPORT"`
	RealtimeUS       string `json:"__REALTIME_TIMESTAMP"`
}

// parseEntry maps a decoded journal entry to our agent-internal
// LogRecord. Severity is taken from PRIORITY (RFC 5424 0..7) using
// the canonical OTel mapping table.
func parseEntry(e journalEntry, probeName string) agentstate.LogRecord {
	pri, _ := strconv.Atoi(e.Priority)
	ts := parseRealtime(e.RealtimeUS)

	attrs := map[string]string{}
	if e.Hostname != "" {
		attrs["host.name"] = e.Hostname
	}
	if e.SystemdUnit != "" {
		attrs["systemd.unit"] = e.SystemdUnit
	}
	if e.SyslogIdentifier != "" {
		attrs["syslog.appname"] = e.SyslogIdentifier
	}
	if e.PID != "" {
		attrs["process.pid"] = e.PID
	}
	if e.UID != "" {
		attrs["process.owner.uid"] = e.UID
	}
	if e.Comm != "" {
		attrs["process.executable.name"] = e.Comm
	}
	if e.Transport != "" {
		attrs["systemd.transport"] = e.Transport
	}

	return agentstate.LogRecord{
		Timestamp:         ts,
		Severity:          agentstate.SyslogPriorityToSeverity(pri),
		SeverityText:      agentstate.SyslogPriorityToText(pri),
		Body:              e.Message,
		Attributes:        attrs,
		ProducerProbeName: probeName,
		ProducerProbeType: "linux_logs",
	}
}

// parseRealtime converts journalctl's __REALTIME_TIMESTAMP (microseconds
// since epoch as a quoted string) into a time.Time. Returns time.Now()
// on parse failure — we'd rather emit with a slightly-off timestamp
// than drop the record.
func parseRealtime(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	usec, err := strconv.ParseInt(s, 10, 64)
	if err != nil || usec <= 0 {
		return time.Now()
	}
	sec := usec / 1_000_000
	nsec := (usec % 1_000_000) * 1_000
	return time.Unix(sec, nsec)
}

// drainReader reads JSON-per-line entries from r, decodes each, and
// publishes a LogRecord. Returns when r reaches EOF or the reader is
// closed (subprocess kill propagates as Read error).
//
// One malformed line is logged and skipped — the journal produces a
// lot of records and a single garbled line should not bring down the
// whole stream.
func drainReader(r *bufio.Reader, log *logger.ModuleLogger, probeName string) {
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\n")
			var entry journalEntry
			if jerr := json.Unmarshal([]byte(line), &entry); jerr != nil {
				log.Debug().Err(jerr).Str("line", truncate(line, 200)).
					Msg("journalctl emitted unparseable line; skipping")
			} else if entry.Message != "" {
				agentstate.PublishLog(parseEntry(entry, probeName))
			}
		}
		if err != nil {
			// io.EOF or "file already closed" on stop — both are
			// expected exit paths.
			return
		}
	}
}

// truncate is a tiny helper for log-line previews so we don't log a
// 4 KB blob just because journalctl emitted something we couldn't
// parse.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// buildJournalctlArgs assembles the command-line for `journalctl`.
// Pulled out as a pure function for testability — start/stop logic
// lives in the OS-specific files.
func buildJournalctlArgs(cfg LinuxLogsProbeConfig) []string {
	args := []string{"--output=json", "--no-pager", "--follow"}
	if !cfg.IncludeBoot {
		args = append(args, "--since=now")
	}
	if cfg.Priority >= 0 && cfg.Priority <= 7 {
		args = append(args, fmt.Sprintf("--priority=%d", cfg.Priority))
	}
	for _, u := range cfg.Units {
		args = append(args, "--unit="+u)
	}
	for _, id := range cfg.Identifiers {
		args = append(args, "--identifier="+id)
	}
	return args
}

// journalReader is the type the probe uses; its concrete fields and
// methods (newJournalReader / stop) are defined per OS in
// journal_reader_linux.go and journal_reader_other.go.
