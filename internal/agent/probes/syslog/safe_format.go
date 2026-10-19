package syslog

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"gopkg.in/mcuadros/go-syslog.v2/format"
)

// safeFormat is syslog.Automatic with its parsers fenced: go-syslog parses
// on the goroutine that reads the socket, with no recover of its own. A
// parse that fails that way is reported as an error and the raw line is
// kept as the message content.
type safeFormat struct {
	format.Automatic
}

func (f *safeFormat) GetParser(line []byte) format.LogParser {
	return &safeParser{LogParser: f.Automatic.GetParser(line), line: string(line)}
}

func (f *safeFormat) GetSplitFunc() bufio.SplitFunc {
	return f.Automatic.GetSplitFunc()
}

type safeParser struct {
	format.LogParser
	line     string
	panicked bool
}

func (p *safeParser) Parse() (err error) {
	defer func() {
		if r := recover(); r != nil {
			p.panicked = true
			err = fmt.Errorf("syslog parser panic: %v", r)
		}
	}()
	return p.LogParser.Parse()
}

func (p *safeParser) Dump() (parts format.LogParts) {
	if strings.TrimSpace(p.line) == "" {
		// An empty frame (a blank line between TCP messages) carries
		// nothing; the library would still fill in default facility and
		// severity, turning it into a record nobody sent.
		return format.LogParts{}
	}
	if p.panicked {
		return format.LogParts{"content": p.line}
	}
	defer func() {
		if r := recover(); r != nil {
			parts = format.LogParts{"content": p.line}
		}
	}()
	return p.LogParser.Dump()
}

func (p *safeParser) Location(loc *time.Location) {
	p.LogParser.Location(loc)
}
