// Package pdh wraps the Windows Performance Data Helper API.
//
// This file holds the parts that are pure string and buffer handling —
// no syscall, no windows package — so they carry no build tag and can be
// tested on any machine. The 562 lines behind the //go:build windows
// guard were wholly untested for exactly that reason: every helper,
// including the ones that never touch a syscall, was unreachable from a
// developer machine and from the Linux CI runners (#297).
package pdh

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"senhub-agent.go/internal/agent/services/logger"
)

// BuildCounterPath inserts an instance into a PDH counter path.
//
// PDH paths look like `\Object\Counter`, and an instance turns them into
// `\Object(instance)\Counter`. A counter path with sub-parts keeps them:
// `\Object\Group\Counter` becomes `\Object(instance)\Group\Counter`.
//
// An empty instance returns the path unchanged, and a path that does not
// have at least an object segment is returned unchanged too — a
// malformed path is the caller's problem to surface, and rewriting it
// into something that looks valid would hide the mistake until PDH
// rejected it with an opaque code.
func BuildCounterPath(path string, instance string) string {
	if instance == "" {
		logDebug("Built path without instance: %s", path)
		return path
	}

	parts := strings.Split(path, "\\")
	if len(parts) >= 2 {
		builtPath := fmt.Sprintf("\\%s(%s)\\%s",
			parts[1],
			instance,
			strings.Join(parts[2:], "\\"))
		logDebug("Built path with instance: %s", builtPath)
		return builtPath
	}

	logDebug("Fallback path: %s", path)
	return path
}

// parseInstanceList decodes the NUL-separated, double-NUL-terminated
// UTF-16 buffer PdhEnumObjectItemsW fills with instance names.
//
// Two entries are dropped: empty strings (the trailing terminator, and
// any padding the API leaves in an over-sized buffer) and `_Total`,
// which is PDH's roll-up row rather than a real instance — summing it
// with the instances it aggregates would double every counter.
func parseInstanceList(buf []uint16) []string {
	var instances []string
	var current []uint16

	for _, char := range buf {
		if char != 0 {
			current = append(current, char)
			continue
		}
		if len(current) > 0 {
			name := string(utf16.Decode(current))
			if name != "" && name != "_Total" {
				instances = append(instances, name)
			}
		}
		current = nil
	}

	// A buffer that ends without its terminator still yields its last
	// name: the API is supposed to double-NUL-terminate, and trusting
	// that would silently drop one instance if it ever did not.
	if len(current) > 0 {
		if name := string(utf16.Decode(current)); name != "" && name != "_Total" {
			instances = append(instances, name)
		}
	}

	return instances
}

var (
	// moduleLogger for PDH operations - initialized with basic logger
	moduleLogger *logger.ModuleLogger
)

// InitializePDHLogger initializes the PDH module logger
func InitializePDHLogger(baseLogger *logger.Logger) {
	moduleLogger = logger.NewModuleLogger(baseLogger, "pdh.windows")
}

// logDebug safely logs debug messages, falling back to no-op if logger not initialized
func logDebug(msg string, args ...interface{}) {
	if moduleLogger != nil {
		if len(args) > 0 {
			moduleLogger.Debug().Msgf(msg, args...)
		} else {
			moduleLogger.Debug().Msg(msg)
		}
	}
	// If moduleLogger is nil, do nothing (silent fallback)
}
