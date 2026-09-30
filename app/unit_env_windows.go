//go:build windows

package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"senhub-agent.go/internal/agent/cliArgs"
)

// serviceEnvironment returns the variables the Windows service receives
// from its registry key (HKLM\...\Services\senhub-agent, value
// Environment), when that service runs the configuration being checked.
// An elevated shell does not have them, so ${env:} references a working
// service resolves were reported as errors (#968).
func serviceEnvironment(configPath string) (map[string]string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, serviceRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer key.Close()

	imagePath, _, _ := key.GetStringValue("ImagePath")
	servicePath := imagePathConfig(imagePath)
	if servicePath == "" {
		if def, derr := cliArgs.GetAbsoluteConfigPath(""); derr == nil {
			servicePath = def
		}
	}
	if servicePath == "" || !strings.EqualFold(filepath.Clean(servicePath), filepath.Clean(configPath)) {
		return nil, nil
	}

	block, _, err := key.GetStringsValue("Environment")
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the service environment: %w", err)
	}
	return parseServiceEnvironmentBlock(block), nil
}
