package app

import "fmt"

// parseConfigPathArgs resolves the config path for `config check` and
// `config show` from their trailing arguments.
//
// Two forms are accepted, because operators type both: a positional path, which
// is what these two commands have always taken, and `--config-path <path>`,
// which is what every other command takes. Before this, `--config-path` was
// silently swallowed AS the path — `config check --config-path /etc/…/agent.yaml`
// reported
//
//	Checking configuration: /usr/local/bin/--config-path
//	[ERROR] Cannot read file: open /usr/local/bin/--config-path: no such file
//
// having resolved a flag name relative to the binary's directory. An unknown
// flag is now an error naming both accepted forms, rather than a path nobody
// asked for: this is the same misfire family as the `--help` one fixed for
// `update` (#134), and it became more likely once the CLI lived in PATH.
func parseConfigPathArgs(args []string) (path string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config-path" || a == "-config-path":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--config-path needs a file path")
			}
			path = args[i+1]
			i++
		case len(a) > 1 && a[0] == '-':
			return "", fmt.Errorf("unknown option %q; pass the config file as a positional path or with --config-path <path>", a)
		default:
			if path != "" {
				return "", fmt.Errorf("more than one config path given (%q and %q)", path, a)
			}
			path = a
		}
	}
	return path, nil
}
