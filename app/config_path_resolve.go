package app

import (
	"path/filepath"
	"strings"

	"senhub-agent.go/internal/agent/cliArgs"
)

const (
	originFlag    = "--config-path"
	originService = "registered service"
	originDefault = "default"
)

// resolveCLIConfigPath decides which configuration file a CLI command uses.
// An explicit flag always wins; otherwise a ZIP-installed service's
// registered ImagePath can point at the config next to the binary; the
// OS-canonical default is the last resort.
func resolveCLIConfigPath(flag string) (path, origin string, err error) {
	if flag != "" {
		p, err := cliArgs.GetAbsoluteConfigPath(flag)
		if err != nil {
			return "", "", err
		}
		return p, originFlag, nil
	}

	if servicePath := serviceConfigPathFromImagePath(registeredServiceImagePath()); servicePath != "" {
		clean := filepath.Clean(servicePath)
		if filepath.IsAbs(clean) {
			return clean, originService, nil
		}
	}

	p, err := cliArgs.GetAbsoluteConfigPath("")
	if err != nil {
		return "", "", err
	}
	return p, originDefault, nil
}

// serviceConfigPathFromImagePath extracts --config-path from a Windows SCM
// ImagePath. It is pure so it can be tested on any OS.
func serviceConfigPathFromImagePath(imagePath string) string {
	args := splitWindowsCommandLine(imagePath)
	if len(args) < 2 {
		return ""
	}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		lower := strings.ToLower(arg)
		if lower == "--config-path" || lower == "-config-path" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(lower, "--config-path=") {
			return arg[len("--config-path="):]
		}
		if strings.HasPrefix(lower, "-config-path=") {
			return arg[len("-config-path="):]
		}
	}
	return ""
}

// splitWindowsCommandLine splits a Windows command line into tokens, removing
// the double quotes that protect spaces in the executable path or a value.
func splitWindowsCommandLine(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote := false
	hasCur := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			hasCur = true
		case (c == ' ' || c == '\t') && !inQuote:
			if hasCur {
				args = append(args, cur.String())
				cur.Reset()
				hasCur = false
			}
		default:
			cur.WriteByte(c)
			hasCur = true
		}
	}
	if hasCur {
		args = append(args, cur.String())
	}
	return args
}
