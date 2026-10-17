package app

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/configuration"
)

// reportOpAMP prints the check lines of the opamp block. The token is a
// credential: no line names it, and the endpoint is printed redacted.
func reportOpAMP(c *configuration.OpAMPConfig) (errors, warnings int) {
	if c == nil {
		return 0, 0
	}
	if !c.Enabled {
		fmt.Println("  [OFF]  opamp: disabled")
		return 0, 0
	}
	if problems := configuration.ValidateOpAMP(c); len(problems) > 0 {
		for _, p := range problems {
			fmt.Printf("  [ERROR] %v\n", p)
		}
		return len(problems), 0
	}
	n := c.Normalized()
	fmt.Printf("  [OK]   opamp: reporting to %s over %s every %s\n", configuration.RedactEndpoint(n.Endpoint), n.Transport, n.HeartbeatInterval)
	if n.TLS.InsecureSkipVerify {
		fmt.Println("  [WARN] opamp.tls.insecure_skip_verify: the server certificate is not verified")
		warnings++
	}
	return 0, warnings
}
