package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
)

// consoleArgs holds what `agent console` accepts.
type consoleArgs struct {
	configPath string
	print      bool
	// handoff is the file an elevated relaunch writes the address to,
	// so the process that asked for it can open the browser with the
	// user's own rights instead of from an elevated context.
	handoff string
}

func parseConsoleArgs(argv []string) (consoleArgs, error) {
	var out consoleArgs
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--print":
			out.print = true
		case "--config-path", "--handoff":
			if i+1 >= len(argv) {
				return out, fmt.Errorf("flag %q needs a value", argv[i])
			}
			if argv[i] == "--config-path" {
				out.configPath = argv[i+1]
			} else {
				out.handoff = argv[i+1]
			}
			i++
		default:
			return out, fmt.Errorf("unknown flag %q", argv[i])
		}
	}
	return out, nil
}

// consoleURL resolves the address of the built-in web console from the
// configuration on disk. The agent key is part of the address and is
// sealed on a modern install, so this needs the rights of the service
// account or an administrator.
func consoleURL(configPath string) (string, error) {
	key, err := extractAgentKeyFromConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("reading the agent key from %s: %w", configPath, err)
	}
	url := buildDashboardURL(configPath, key)
	if url == "" {
		return "", fmt.Errorf("no agent key in %s", configPath)
	}
	return url, nil
}

// waitForEndpoint gives a freshly started service the time to bind
// before a browser is pointed at it. The MSI opens the console right
// after the service start; without this the first page is an error.
func waitForEndpoint(configPath string, timeout time.Duration) bool {
	_, port := resolveHTTPStrategyEndpoint(configPath)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// runConsole implements `agent console`: print the console address, or
// open it in the default browser.
func runConsole(argv []string) {
	opts, err := parseConsoleArgs(argv)
	if err != nil {
		fatalf("console: %v", err)
	}
	configPath := opts.configPath
	if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
		configPath = resolved
	}

	url, err := consoleURL(configPath)
	if err != nil {
		if opts.handoff != "" || !canRelaunchElevated() {
			fatalf("console: %v", err)
		}
		// The key is readable by administrators only; ask once, then
		// come back with the address and open it as the user.
		url, err = elevatedConsoleURL(configPath)
		if err != nil {
			fatalf("console: %v", err)
		}
	}

	if opts.handoff != "" {
		if err := os.WriteFile(opts.handoff, []byte(url), 0o600); err != nil {
			fatalf("console: writing the address for the caller: %v", err)
		}
		return
	}

	fmt.Println(url)
	if opts.print {
		return
	}
	if !waitForEndpoint(configPath, 15*time.Second) {
		fmt.Fprintln(os.Stderr, "Warning: the agent does not answer on this address yet; check the service with: senhub-agent status")
	}
	if err := openBrowser(url); err != nil {
		fatalf("console: opening the browser: %v", err)
	}
}

var errNoElevation = errors.New("the agent key is readable by administrators only; run this command as root or administrator")
