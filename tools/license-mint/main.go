// Local dev tool — mint a compact license for testing.
// Build: go run ./tools/license-mint <tier> <subject> <probe1,probe2,...>
// Example: go run ./tools/license-mint pro sha901-test cpu,memory,ibmi
//
// Not built into the main agent binary. Stays under tools/.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/license"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: license-mint <free|pro|enterprise> <subject> [probe1,probe2,...]")
		os.Exit(2)
	}

	tier := license.LicenseTier(os.Args[1])
	subject := os.Args[2]

	var probes []string
	if len(os.Args) > 3 {
		for _, p := range strings.Split(os.Args[3], ",") {
			if p = strings.TrimSpace(p); p != "" {
				probes = append(probes, p)
			}
		}
	}

	expires := time.Now().Add(365 * 24 * time.Hour)
	key, err := license.GenerateCompactLicense(tier, expires, probes, subject)
	if err != nil {
		fmt.Fprintln(os.Stderr, "license-mint:", err)
		os.Exit(1)
	}
	fmt.Println(key)
}
