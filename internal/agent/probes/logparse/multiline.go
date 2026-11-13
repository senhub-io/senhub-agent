package logparse

// Assembler folds physical lines into logical records per the
// MultilineConfig. It is stateful per file: a partial record may span
// several Append calls. Not safe for concurrent use — one assembler per
// tailed file.
//
// Model (Match == "after", the default): a physical line that matches
// the pattern starts a NEW logical record; lines that do not match are
// continuations appended to the record in progress. This matches the
// canonical "pattern = leading timestamp of a new message" case from
// the issue.
//
// Match == "before": a matching line is the LAST line of the current
// record — it is appended and then the record is flushed.
//
// Negate inverts the match test.
type Assembler struct {
	cfg     MultilineConfig
	maxLen  int
	pending []string
	curLen  int
}

// NewAssembler builds an assembler for cfg; maxLen caps a logical record
// (0 means DefaultMaxBytesPerLine).
func NewAssembler(cfg MultilineConfig, maxLen int) *Assembler {
	if maxLen <= 0 {
		maxLen = DefaultMaxBytesPerLine
	}
	return &Assembler{cfg: cfg, maxLen: maxLen}
}

// enabled reports whether multiline folding is active. When false the
// assembler is a pass-through: every Append returns the line as its own
// record.
func (a *Assembler) enabled() bool {
	return a.cfg.compiled != nil
}

func (a *Assembler) matches(line string) bool {
	if a.cfg.compiled == nil {
		return false
	}
	m := a.cfg.compiled.MatchString(line)
	if a.cfg.Negate {
		return !m
	}
	return m
}

// Append feeds one physical line. It returns the set of completed
// logical records produced by this line (usually zero or one). The
// records are already truncated to maxLen.
func (a *Assembler) Append(line string) []string {
	if !a.enabled() {
		return []string{Truncate(line, a.maxLen)}
	}

	if a.cfg.Match == "before" {
		var out []string
		if a.full(line) {
			out = a.flush()
		}
		a.push(line)
		if a.matches(line) {
			out = append(out, a.flush()...)
		}
		return out
	}

	// Match == "after" (default).
	if a.matches(line) {
		out := a.flush()
		a.push(line)
		return out
	}
	// A continuation with nothing started (the file opened mid-message,
	// or the start was already sent by an idle flush) opens a record of
	// its own: the continuations after it fold into it, as they would
	// into a record that had its first line.
	if a.full(line) {
		// The record would pass the cap: send it, and carry on with this
		// line as the start of the next one, rather than dropping every
		// line past the cap until the next record starts.
		out := a.flush()
		a.push(line)
		return out
	}
	a.push(line)
	return nil
}

// full reports whether line would take the record in progress past the
// cap. A record is cut between lines, so no byte is lost; only a single
// line longer than the cap is truncated.
func (a *Assembler) full(line string) bool {
	return len(a.pending) > 0 && a.curLen+len(line) > a.maxLen
}

// Pending reports whether a logical record is being assembled, so a caller
// that carries per-record metadata knows which line opened it.
func (a *Assembler) Pending() bool { return len(a.pending) > 0 }

// Flush returns any record still being assembled. Called when the file
// goes idle / on shutdown so a trailing partial message is not lost.
func (a *Assembler) Flush() []string {
	if !a.enabled() {
		return nil
	}
	return a.flush()
}

func (a *Assembler) push(line string) {
	a.pending = append(a.pending, line)
	a.curLen += len(line) + 1 // +1 for the joining newline
}

func (a *Assembler) flush() []string {
	if len(a.pending) == 0 {
		return nil
	}
	joined := joinLines(a.pending)
	a.pending = a.pending[:0]
	a.curLen = 0
	return []string{Truncate(joined, a.maxLen)}
}

func joinLines(lines []string) string {
	if len(lines) == 1 {
		return lines[0]
	}
	out := lines[0]
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out
}

// Truncate cuts s to max bytes; max <= 0 means no cap.
func Truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
