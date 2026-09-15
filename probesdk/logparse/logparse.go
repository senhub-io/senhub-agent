// Package logparse is the public mirror of the agent's log line parsing
// (senhub-agent.go/internal/agent/probes/logparse): the raw / regex /
// json / logfmt parsers, the multiline assembler and their params
// blocks. A probe that reads lines from somewhere and publishes them on
// the log rail (see probesdk/logs) uses this mirror because Go forbids
// importing senhub-agent.go/internal/... across module boundaries.
package logparse

import ilp "senhub-agent.go/internal/agent/probes/logparse"

type (
	ParserType      = ilp.ParserType
	ParserConfig    = ilp.ParserConfig
	MultilineConfig = ilp.MultilineConfig
	Assembler       = ilp.Assembler
)

const (
	ParserRaw    = ilp.ParserRaw
	ParserRegex  = ilp.ParserRegex
	ParserJSON   = ilp.ParserJSON
	ParserLogfmt = ilp.ParserLogfmt

	DefaultMaxBytesPerLine = ilp.DefaultMaxBytesPerLine
)

// ParseParser reads the optional `parser` block of a probe's params.
func ParseParser(config map[string]interface{}, pc *ParserConfig) error {
	return ilp.ParseParser(config, pc)
}

// ParseMultiline reads the optional `multiline` block of a probe's params.
func ParseMultiline(config map[string]interface{}, ml *MultilineConfig) error {
	return ilp.ParseMultiline(config, ml)
}

// NewAssembler builds a multiline assembler; one per line source.
func NewAssembler(cfg MultilineConfig, maxLen int) *Assembler { return ilp.NewAssembler(cfg, maxLen) }

// ParseLine turns one logical line into a log record per the parser config.
var ParseLine = ilp.ParseLine

// Truncate cuts s to max bytes; max <= 0 means no cap.
func Truncate(s string, max int) string { return ilp.Truncate(s, max) }
