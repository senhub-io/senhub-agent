package app

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/cliexit"
)

// settableKey describes one `config set` key: which strategy fragment and
// param it edits, the YAML tag to write it with, and a validator. The set is
// deliberately small — it grows one reviewed key at a time rather than
// exposing the whole schema, so `config set` cannot write a value the agent
// then refuses at load.
type settableKey struct {
	strategy string
	param    string
	tag      string
	validate func(string) error
}

var settableKeys = map[string]settableKey{
	"http.port": {
		strategy: "http", param: "port", tag: "!!int",
		validate: func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("must be a number, got %q", v)
			}
			if n < 1 || n > 65535 {
				return fmt.Errorf("must be between 1 and 65535, got %d", n)
			}
			return nil
		},
	},
	"http.bind_address": {
		strategy: "http", param: "bind_address", tag: "!!str",
		validate: func(v string) error {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("must not be empty")
			}
			return nil
		},
	},
}

type configSetReport struct {
	jsonHeader
	Key        string `json:"key"`
	Value      string `json:"value"`
	ConfigPath string `json:"config_path"`
	File       string `json:"file,omitempty"`
	Changed    bool   `json:"changed"`
}

// runConfigSet implements `config set <key> <value> [--json]`: change one
// setting in the multi-file layout without hand-editing YAML. The running
// agent reloads it through the config watcher, so no restart is needed. A
// setting that already holds the value is not written again and the
// command exits with the Unchanged code.
func runConfigSet(argv []string, out io.Writer) int {
	argv, jsonMode := extractJSONFlag(argv)
	fail := func(err error) int {
		return reportFailure("config.set", jsonMode, out, err)
	}

	var key, value, configPath string
	var positional []string
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--config-path" {
			if i+1 >= len(argv) {
				return fail(errors.New("config set: --config-path needs a value"))
			}
			configPath = argv[i+1]
			i++
			continue
		}
		positional = append(positional, argv[i])
	}
	if len(positional) != 2 {
		return fail(fmt.Errorf("config set: expected <key> <value>, e.g. 'config set http.port 9080'\nknown keys: %s", strings.Join(sortedSettableKeys(), ", ")))
	}
	key, value = positional[0], positional[1]

	spec, ok := settableKeys[key]
	if !ok {
		return fail(fmt.Errorf("config set: unknown key %q; known keys: %s", key, strings.Join(sortedSettableKeys(), ", ")))
	}
	if err := spec.validate(value); err != nil {
		return fail(fmt.Errorf("config set %s: %w", key, err))
	}

	if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
		configPath = resolved
	}
	if configPath == "" {
		return fail(errors.New("config set: could not resolve a config path"))
	}

	fragment, changed, err := configuration.SetStrategyScalarReport(configPath, spec.strategy, spec.param, value, spec.tag)
	if err != nil {
		return fail(fmt.Errorf("config set %s: %w", key, err))
	}

	code := cliexit.OK
	if !changed {
		code = cliexit.Unchanged
	}
	if jsonMode {
		report := configSetReport{
			jsonHeader: newJSONHeader("config.set", code),
			Key:        key,
			Value:      value,
			ConfigPath: configPath,
			File:       fragment,
			Changed:    changed,
		}
		if err := writeJSON(out, report); err != nil {
			return reportFailure("config.set", false, out, err)
		}
		return code
	}
	if !changed {
		fmt.Fprintf(out, "%s is already %s; nothing to do.\n", key, value)
		return code
	}
	fmt.Fprintf(out, "Set %s = %s\n", key, value)
	fmt.Fprintln(out, "The running agent reloads the change on its own; no restart needed.")
	return code
}

func sortedSettableKeys() []string {
	keys := make([]string, 0, len(settableKeys))
	for k := range settableKeys {
		keys = append(keys, k)
	}
	// small, fixed set — a simple insertion sort keeps output stable
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
