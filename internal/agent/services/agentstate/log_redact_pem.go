package agentstate

import (
	"regexp"
	"strings"
	"sync"
)

var (
	pemBeginRe      = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	pemEndRe        = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	pemEndMaskRe    = regexp.MustCompile(`^[\s\S]*?(-----END [A-Z0-9 ]*PRIVATE KEY-----)`)
	pemBase64LineRe = regexp.MustCompile(`^[A-Za-z0-9+/=]+$`)
)

// pemTracker remembers the sources whose last record opened a private key
// block without closing it: a file read without folding delivers each PEM
// line as its own record, which no per-record pattern can see. An entry
// lives only while the block is open and is capped, so a stray BEGIN line
// cannot mask unrelated records for long.
type pemTracker struct {
	mu    sync.Mutex
	state map[string]int
}

var pemState pemTracker

const pemMaxBodyRecords = 200

func sourceKey(rec LogRecord) string {
	attrs := rec.Attributes
	return rec.ProducerProbeName + "\x00" +
		attrs["log.file.path"] + "\x00" +
		attrs["log.iostream"] + "\x00" +
		attrs["systemd.unit"] + "\x00" +
		attrs["container.name"] + "\x00" +
		attrs["syslog.hostname"] + "\x00" +
		attrs["syslog.appname"]
}

func (t *pemTracker) reset() {
	t.mu.Lock()
	t.state = nil
	t.mu.Unlock()
}

func (t *pemTracker) len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.state)
}

func (t *pemTracker) process(rec LogRecord, enabled, opens bool) (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !enabled {
		t.state = nil
		return false, ""
	}

	key := sourceKey(rec)
	_, active := t.state[key]

	if active {
		if pemEndRe.MatchString(rec.Body) {
			masked := pemEndMaskRe.ReplaceAllString(rec.Body, "***${1}")
			delete(t.state, key)
			recordRedaction(rec.ProducerProbeName, "private_key")
			return true, masked
		}

		trimmed := strings.TrimSpace(rec.Body)
		if isPEMBodyLine(trimmed) {
			remaining := t.state[key] - 1
			if remaining == 0 {
				delete(t.state, key)
			} else {
				t.state[key] = remaining
			}
			recordRedaction(rec.ProducerProbeName, "private_key")
			return true, "***"
		}

		delete(t.state, key)
	}

	if opens {
		if t.state == nil {
			t.state = make(map[string]int)
		}
		t.state[key] = pemMaxBodyRecords
	}

	return false, ""
}

func isPEMBodyLine(trimmed string) bool {
	return trimmed == "" ||
		pemBase64LineRe.MatchString(trimmed) ||
		strings.HasPrefix(trimmed, "Proc-Type:") ||
		strings.HasPrefix(trimmed, "DEK-Info:")
}

func hasRedactRule(rs *redactionRuleset, name string) bool {
	if rs == nil {
		return false
	}
	for i := range rs.rules {
		if rs.rules[i].name == name {
			return true
		}
	}
	return false
}

func hasUnclosedPEMBegin(body string) bool {
	begins := pemBeginRe.FindAllStringIndex(body, -1)
	for _, begin := range begins {
		if !pemEndRe.MatchString(body[begin[1]:]) {
			return true
		}
	}
	return false
}
