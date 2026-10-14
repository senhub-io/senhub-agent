package podlogs

import (
	"sort"
	"strings"
	"time"
)

// DefaultMaxBytes caps a single reassembled log record when MaxBytes is zero.
const DefaultMaxBytes = 256 << 10

// Assembler folds partial CRI/Docker chunks into complete logical messages.
// It is stateful per file and not safe for concurrent use.
type Assembler struct {
	// MaxBytes caps the length of an assembled message; zero means DefaultMaxBytes.
	MaxBytes int

	runs map[string]*partialRun
}

type partialRun struct {
	firstTime time.Time
	buf       strings.Builder
	length    int
	truncated bool
}

// Add feeds one parsed entry. Partial entries are buffered per stream; a full
// entry closes a run for its own stream and returns the complete entry.
func (a *Assembler) Add(e Entry) (Entry, bool) {
	max := a.max()

	if e.Partial {
		if a.runs == nil {
			a.runs = make(map[string]*partialRun)
		}
		r := a.runs[e.Stream]
		if r == nil {
			r = &partialRun{firstTime: e.Time}
			a.runs[e.Stream] = r
		}
		r.append(e.Message, max)
		return Entry{}, false
	}

	if r := a.runs[e.Stream]; r != nil {
		r.append(e.Message, max)
		delete(a.runs, e.Stream)
		return truncateEntry(Entry{
			Time:      r.firstTime,
			Stream:    e.Stream,
			Message:   r.buf.String(),
			Truncated: r.truncated,
		}, max), true
	}

	return truncateEntry(e, max), true
}

// Flush returns any pending partial runs, marking them as partial because no
// full chunk has closed them. It is intended for file rotation or shutdown.
func (a *Assembler) Flush() []Entry {
	if len(a.runs) == 0 {
		return nil
	}

	max := a.max()
	streams := make([]string, 0, len(a.runs))
	for stream := range a.runs {
		streams = append(streams, stream)
	}
	sort.Strings(streams)

	out := make([]Entry, 0, len(streams))
	for _, stream := range streams {
		r := a.runs[stream]
		out = append(out, truncateEntry(Entry{
			Time:      r.firstTime,
			Stream:    stream,
			Partial:   true,
			Message:   r.buf.String(),
			Truncated: r.truncated,
		}, max))
		delete(a.runs, stream)
	}
	return out
}

func (a *Assembler) max() int {
	if a.MaxBytes <= 0 {
		return DefaultMaxBytes
	}
	return a.MaxBytes
}

func (r *partialRun) append(msg string, max int) {
	if r.length >= max {
		if len(msg) > 0 {
			r.truncated = true
		}
		return
	}

	if len(msg) > max-r.length {
		r.buf.WriteString(msg[:max-r.length])
		r.length = max
		r.truncated = true
		return
	}

	r.buf.WriteString(msg)
	r.length += len(msg)
}

func truncateEntry(e Entry, max int) Entry {
	if len(e.Message) <= max {
		return e
	}
	e.Message = e.Message[:max]
	e.Truncated = true
	return e
}
