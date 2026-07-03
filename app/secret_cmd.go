package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/configuration/secret"
)

// The `secret` verb manages the OS-native secret store that backs ${secret:}
// references in the config. It is NOT ReadOnly: it reads and writes the
// root-owned store, so it runs behind the privilege gate. A secret VALUE is
// never accepted on the command line (it would leak via ps / shell history);
// `set` reads it from a hidden prompt, stdin, or --from-file.
func init() {
	RegisterCommand(ExtraCommand{Name: "secret", ReadOnly: false, Run: runSecretCommand})
}

func secretUsage() {
	fmt.Fprint(os.Stderr, `Usage: agent secret <command> [name]

  set <name>        store/replace a secret (hidden prompt, or stdin, or --from-file <path>)
  get <name>        print a secret value to stdout (deliberate reveal)
  list              list secret names (never values)
  rm <name>         delete a secret (prompts to confirm; --yes to skip)
  migrate           move inline plaintext secrets from the config into the store
  wire-unit         (Linux/systemd-creds) regenerate the unit credential drop-in
  status            show the active backend and store location

The value of a secret is never read from the command line.
`)
}

func runSecretCommand() {
	args := os.Args[2:]
	if len(args) == 0 {
		secretUsage()
		os.Exit(2)
	}

	configDir, err := secretConfigDir(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := secret.InitRegistry(configDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: initialising secret backend: %v\n", err)
		os.Exit(1)
	}
	p := secret.ActiveProvider()
	if p == nil {
		fmt.Fprintln(os.Stderr, "Error: no secret backend available on this host")
		os.Exit(1)
	}

	sub := args[0]
	switch sub {
	case "status":
		fmt.Printf("backend: %s\nstore:   %s\n", p.Name(), configDir)
		names, _ := p.List()
		fmt.Printf("secrets: %d\n", len(names))

	case "list":
		names, err := p.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, n := range names {
			fmt.Println(n)
		}

	case "set":
		name := secretArgName(args)
		val, err := readSecretValue(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := p.Set(name, secret.New(val)); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("stored secret %q in %s; reference it as ${secret:%s}\n", name, p.Name(), name)

	case "get":
		name := secretArgName(args)
		v, err := p.Get(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Fprintln(os.Stderr, "warning: writing a secret value to a non-terminal")
		}
		fmt.Println(v)

	case "rm":
		name := secretArgName(args)
		if !secretHasFlag(args, "--yes") {
			fmt.Printf("Remove secret %q? [y/N] ", name)
			if !readYesConfirmation() {
				fmt.Println("Cancelled.")
				return
			}
		}
		if err := p.Delete(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("removed secret %q\n", name)

	case "migrate":
		cfgPath, err := secretConfigFile(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := configuration.SealInlineSecrets(cfgPath, nil); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("sealed inline secrets into the store and rewrote them to ${secret:} references")

	case "wire-unit":
		if err := wireSystemdUnit(configDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	default:
		secretUsage()
		os.Exit(2)
	}
}

// secretArgName extracts the <name> positional (the first arg after the
// sub-verb that is not a flag).
func secretArgName(args []string) string {
	for _, a := range args[1:] {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	fmt.Fprintln(os.Stderr, "Error: missing <name>")
	os.Exit(2)
	return ""
}

// secretHasFlag reports whether the given boolean flag is present anywhere in
// the secret sub-command arguments.
func secretHasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// secretConfigDir returns the directory holding the agent config (and thus the
// secret store). It honours an explicit `--config-path <path>` like the other
// config subcommands, falling back to the canonical OS path.
func secretConfigDir(args []string) (string, error) {
	cp := ""
	for i, a := range args {
		if a == "--config-path" && i+1 < len(args) {
			cp = args[i+1]
		}
	}
	path, err := cliArgs.GetAbsoluteConfigPath(cp)
	if err != nil {
		return "", fmt.Errorf("resolving config path: %w", err)
	}
	return filepath.Dir(path), nil
}

// secretConfigFile resolves the config FILE path (honouring --config-path),
// used by `secret migrate`.
func secretConfigFile(args []string) (string, error) {
	cp := ""
	for i, a := range args {
		if a == "--config-path" && i+1 < len(args) {
			cp = args[i+1]
		}
	}
	path, err := cliArgs.GetAbsoluteConfigPath(cp)
	if err != nil {
		return "", fmt.Errorf("resolving config path: %w", err)
	}
	return path, nil
}

// readSecretValue obtains the secret value WITHOUT taking it from argv:
// from --from-file <path>, else a hidden terminal prompt, else a line of stdin.
func readSecretValue(args []string) (string, error) {
	for i, a := range args {
		if a == "--from-file" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("--from-file needs a path")
			}
			data, err := os.ReadFile(args[i+1])
			if err != nil {
				return "", fmt.Errorf("reading --from-file: %w", err)
			}
			return strings.TrimRight(string(data), "\r\n"), nil
		}
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Secret value (hidden): ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("reading secret: %w", err)
		}
		return string(b), nil
	}
	// Non-interactive: read one line from stdin (the documented piping path).
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("reading secret from stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
