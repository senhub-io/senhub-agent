package agentstate

import (
	"regexp"
	"sync"
	"sync/atomic"
)

var (
	pemBeginRe = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	pemEndRe   = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

const (
	pemMaxBodyRecords = 200
	pemMaxBodyBytes   = 64 << 10
)

// pemBlock is the open private key block of one source: the records it
// may still mask and what it has masked so far.
type pemBlock struct {
	remaining int
	masked    int
	bytes     int
}

// pemTracker remembers the sources whose last record opened a private key
// block without closing it: a file read without folding delivers each PEM
// line as its own record, which no per-record pattern can see. While a
// block is open every record of its source is masked, whatever it holds,
// until the END line. The block is bounded in records and bytes, so a
// stray BEGIN line cannot mask a source for long.
type pemTracker struct {
	mu    sync.Mutex
	state map[string]*pemBlock
}

var pemState, batchPemState pemTracker

// PEMOverflowHandler is told that a private key block was abandoned
// because it reached its bound without an END line.
type PEMOverflowHandler func(probe, source string, records, bytes int)

var pemOverflowHandler atomic.Pointer[PEMOverflowHandler]

// SetPEMOverflowHandler installs the handler told when a source's private
// key block is abandoned at its bound; nil removes it.
func SetPEMOverflowHandler(fn PEMOverflowHandler) {
	if fn == nil {
		pemOverflowHandler.Store(nil)
		return
	}
	pemOverflowHandler.Store(&fn)
}

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

func sourceDescription(rec LogRecord) string {
	for _, k := range []string{"log.file.path", "log.iostream", "systemd.unit", "container.name", "syslog.appname", "syslog.hostname"} {
		if v := rec.Attributes[k]; v != "" {
			return v
		}
	}
	return ""
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

type pemOverflow struct {
	probe, source  string
	records, bytes int
}

// process masks rec when its source has an open private key block and
// reports whether it did. mask applies the other rules to the text that
// follows an END line in the same record.
func (t *pemTracker) process(rec LogRecord, enabled, opens bool, mask func(string) string) (bool, string) {
	masked, body, overflow := t.step(rec, enabled, opens, mask)
	// The handler logs, and the agent's own log records come back through
	// this tracker: calling it under t.mu would deadlock.
	if overflow != nil {
		if h := pemOverflowHandler.Load(); h != nil {
			(*h)(overflow.probe, overflow.source, overflow.records, overflow.bytes)
		}
	}
	return masked, body
}

func (t *pemTracker) step(rec LogRecord, enabled, opens bool, mask func(string) string) (bool, string, *pemOverflow) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !enabled {
		t.state = nil
		return false, "", nil
	}

	key := sourceKey(rec)
	block, active := t.state[key]

	if !active {
		if opens {
			t.open(key)
		}
		return false, "", nil
	}

	recordRedaction(rec.ProducerProbeName, "private_key")

	if loc := pemEndRe.FindStringIndex(rec.Body); loc != nil {
		delete(t.state, key)
		tail := rec.Body[loc[1]:]
		if hasUnclosedPEMBegin(tail) {
			t.open(key)
		}
		if mask != nil {
			tail = mask(tail)
		}
		return true, "***" + rec.Body[loc[0]:loc[1]] + tail, nil
	}

	block.remaining--
	block.masked++
	block.bytes += len(rec.Body)
	if block.remaining > 0 && block.bytes < pemMaxBodyBytes {
		return true, "***", nil
	}

	delete(t.state, key)
	return true, "***", &pemOverflow{
		probe:   rec.ProducerProbeName,
		source:  sourceDescription(rec),
		records: block.masked,
		bytes:   block.bytes,
	}
}

func (t *pemTracker) open(key string) {
	if t.state == nil {
		t.state = make(map[string]*pemBlock)
	}
	t.state[key] = &pemBlock{remaining: pemMaxBodyRecords}
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
