package app

import (
	"fmt"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
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

// runConfigSet implements `config set <key> <value>`: change one setting in
// the multi-file layout without hand-editing YAML. The running agent reloads
// it through the config watcher, so no restart is needed.
func runConfigSet(argv []string) {
	var key, value, configPath string
	var positional []string
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--config-path" {
			if i+1 >= len(argv) {
				fatalf("config set: --config-path needs a value")
			}
			configPath = argv[i+1]
			i++
			continue
		}
		positional = append(positional, argv[i])
	}
	if len(positional) != 2 {
		fatalf("config set: expected <key> <value>, e.g. 'config set http.port 9080'\nknown keys: %s", strings.Join(sortedSettableKeys(), ", "))
	}
	key, value = positional[0], positional[1]

	spec, ok := settableKeys[key]
	if !ok {
		fatalf("config set: unknown key %q; known keys: %s", key, strings.Join(sortedSettableKeys(), ", "))
	}
	if err := spec.validate(value); err != nil {
		fatalf("config set %s: %v", key, err)
	}

	if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
		configPath = resolved
	}
	if configPath == "" {
		fatalf("config set: could not resolve a config path")
	}

	if err := configuration.SetStrategyScalar(configPath, spec.strategy, spec.param, value, spec.tag); err != nil {
		fatalf("config set %s: %v", key, err)
	}
	fmt.Printf("Set %s = %s\n", key, value)
	fmt.Println("The running agent reloads the change on its own; no restart needed.")
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
