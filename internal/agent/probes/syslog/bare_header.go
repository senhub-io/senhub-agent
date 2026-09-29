package syslog

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// bareHeader is an RFC 3164 header sent without its <PRI> part, as the
// UniFi controller does for its CEF activity log. go-syslog then applies
// the default priority 13 and keeps the whole line, header included, as
// content; the timestamp and hostname are recovered here.
type bareHeader struct {
	timestamp time.Time
	hostname  string
	tag       string
	rest      string
}

// bareTag is the "app:" or "app[pid]:" that opens an RFC 3164 message.
// A CEF record ("CEF:0|...") is not a tag and stays whole.
var bareTag = regexp.MustCompile(`^([A-Za-z0-9_./-]{1,48})(?:\[[0-9]+\])?: `)

// parseBareHeader recognises "Mmm dd hh:mm:ss host rest". A hostname
// ending in ':' is a tag with no hostname before it, which go-syslog
// would have parsed had a PRI been present, so it is not taken.
func parseBareHeader(content string, now time.Time) (bareHeader, bool) {
	if len(content) <= len(time.Stamp)+1 || content[len(time.Stamp)] != ' ' {
		return bareHeader{}, false
	}
	ts, err := time.ParseInLocation(time.Stamp, content[:len(time.Stamp)], now.Location())
	if err != nil {
		return bareHeader{}, false
	}
	host, rest, ok := strings.Cut(content[len(time.Stamp)+1:], " ")
	if !ok || host == "" || strings.HasSuffix(host, ":") || rest == "" {
		return bareHeader{}, false
	}

	// The BSD timestamp carries no year: take the current one, and the
	// previous one for a December line read in early January.
	ts = ts.AddDate(now.Year(), 0, 0)
	if ts.After(now.Add(24 * time.Hour)) {
		ts = ts.AddDate(-1, 0, 0)
	}
	hdr := bareHeader{timestamp: ts, hostname: host, rest: rest}
	if m := bareTag.FindStringSubmatch(rest); m != nil && m[1] != "CEF" {
		hdr.tag, hdr.rest = m[1], rest[len(m[0]):]
	}
	return hdr, true
}

// cefSeverity maps the severity field of a CEF record
// ("CEF:Version|Vendor|Product|Version|ID|Name|Severity|Extension") to
// a syslog severity code. CEF allows 0-10 or Low, Medium, High,
// Very-High.
func cefSeverity(content string) (int, bool) {
	if !strings.HasPrefix(content, "CEF:") {
		return 0, false
	}
	fields := strings.SplitN(content, "|", 8)
	if len(fields) < 8 {
		return 0, false
	}
	level := strings.TrimSpace(fields[6])
	if n, err := strconv.Atoi(level); err == nil {
		switch {
		case n < 0 || n > 10:
			return 0, false
		case n <= 3:
			return 6, true // informational
		case n <= 6:
			return 4, true // warning
		case n <= 8:
			return 3, true // error
		default:
			return 2, true // critical
		}
	}
	switch strings.ToLower(level) {
	case "low":
		return 6, true
	case "medium":
		return 4, true
	case "high":
		return 3, true
	case "very-high":
		return 2, true
	}
	return 0, false
}
