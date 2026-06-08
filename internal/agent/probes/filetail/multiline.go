package filetail

// multilineAssembler folds physical lines into logical records per the
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
type multilineAssembler struct {
	cfg     MultilineConfig
	maxLen  int
	pending []string
	curLen  int
}

func newMultilineAssembler(cfg MultilineConfig, maxLen int) *multilineAssembler {
	if maxLen <= 0 {
		maxLen = DefaultMaxBytesPerLine
	}
	return &multilineAssembler{cfg: cfg, maxLen: maxLen}
}

// enabled reports whether multiline folding is active. When false the
// assembler is a pass-through: every Append returns the line as its own
// record.
func (a *multilineAssembler) enabled() bool {
	return a.cfg.compiled != nil
}

func (a *multilineAssembler) matches(line string) bool {
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
func (a *multilineAssembler) Append(line string) []string {
	if !a.enabled() {
		return []string{truncate(line, a.maxLen)}
	}

	if a.cfg.Match == "before" {
		a.push(line)
		if a.matches(line) {
			return a.flush()
		}
		return nil
	}

	// Match == "after" (default).
	if a.matches(line) {
		out := a.flush()
		a.push(line)
		return out
	}
	if len(a.pending) == 0 {
		// Continuation with nothing started yet (file opens mid-stack):
		// emit the orphan line on its own rather than swallow it.
		return []string{truncate(line, a.maxLen)}
	}
	a.push(line)
	return nil
}

// Flush returns any record still being assembled. Called when the file
// goes idle / on shutdown so a trailing partial message is not lost.
func (a *multilineAssembler) Flush() []string {
	if !a.enabled() {
		return nil
	}
	return a.flush()
}

func (a *multilineAssembler) push(line string) {
	if a.curLen >= a.maxLen {
		return // already at cap; drop further continuation bytes
	}
	a.pending = append(a.pending, line)
	a.curLen += len(line) + 1 // +1 for the joining newline
}

func (a *multilineAssembler) flush() []string {
	if len(a.pending) == 0 {
		return nil
	}
	joined := joinLines(a.pending)
	a.pending = a.pending[:0]
	a.curLen = 0
	return []string{truncate(joined, a.maxLen)}
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

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
