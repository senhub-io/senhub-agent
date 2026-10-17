package configuration

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// OpAMPConfig is the agent.yaml `opamp:` block.
type OpAMPConfig struct {
	Enabled           bool           `yaml:"enabled"`
	Endpoint          string         `yaml:"endpoint,omitempty"`
	Token             string         `yaml:"token,omitempty"`
	Transport         string         `yaml:"transport,omitempty"`
	TLS               OpAMPTLSConfig `yaml:"tls,omitempty"`
	HeartbeatInterval string         `yaml:"heartbeat_interval,omitempty"`
}

// OpAMPTLSConfig is the `opamp.tls:` block.
type OpAMPTLSConfig struct {
	CAFile             string `yaml:"ca_file,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty"`
}

const (
	OpAMPTransportWebSocket       = "websocket"
	OpAMPTransportHTTP            = "http"
	DefaultOpAMPHeartbeatInterval = 30 * time.Second
	MinOpAMPHeartbeatInterval     = time.Second
)

// Normalized returns a copy with normalized, trimmed settings.
func (c OpAMPConfig) Normalized() OpAMPConfig {
	out := c
	out.Endpoint = strings.TrimSpace(c.Endpoint)
	out.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	out.HeartbeatInterval = strings.TrimSpace(c.HeartbeatInterval)
	out.TLS.CAFile = strings.TrimSpace(c.TLS.CAFile)

	if out.Transport == "" {
		out.Transport = OpAMPTransportWebSocket
	}
	if out.HeartbeatInterval == "" {
		out.HeartbeatInterval = DefaultOpAMPHeartbeatInterval.String()
	}

	return out
}

// Heartbeat parses the normalized heartbeat interval.
func (c OpAMPConfig) Heartbeat() (time.Duration, error) {
	normalized := c.Normalized()

	d, err := time.ParseDuration(normalized.HeartbeatInterval)
	if err != nil {
		return 0, fmt.Errorf("opamp.heartbeat_interval %q: %w", normalized.HeartbeatInterval, err)
	}
	if d < MinOpAMPHeartbeatInterval {
		return 0, fmt.Errorf("opamp.heartbeat_interval %q: must be at least 1s", normalized.HeartbeatInterval)
	}

	return d, nil
}

// PlaintextAllowed reports whether a plaintext endpoint may be used.
func (c OpAMPConfig) PlaintextAllowed() bool {
	normalized := c.Normalized()

	u, err := url.Parse(normalized.Endpoint)
	if err != nil {
		return false
	}
	if normalized.TLS.InsecureSkipVerify {
		return true
	}

	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ValidateOpAMP validates an enabled OpAMP configuration and returns every
// problem found.
func ValidateOpAMP(c *OpAMPConfig) []error {
	if c == nil || !c.Enabled {
		return nil
	}

	normalized := c.Normalized()
	var errs []error

	if normalized.Endpoint == "" {
		errs = append(errs, fmt.Errorf("opamp.endpoint is required when opamp.enabled is true"))
	}

	if strings.TrimSpace(normalized.Token) == "" {
		errs = append(errs, fmt.Errorf("opamp.token is required when opamp.enabled is true"))
	}

	transportValid := normalized.Transport == OpAMPTransportWebSocket || normalized.Transport == OpAMPTransportHTTP
	if !transportValid {
		errs = append(errs, fmt.Errorf("opamp.transport %q is not supported (accepted: websocket, http)", normalized.Transport))
	}

	if normalized.Endpoint != "" {
		u, err := url.Parse(normalized.Endpoint)
		if err != nil {
			errs = append(errs, fmt.Errorf("opamp.endpoint: invalid endpoint"))
		} else if u.Hostname() == "" {
			errs = append(errs, fmt.Errorf("opamp.endpoint: URL must include a host"))
		} else {
			scheme := strings.ToLower(u.Scheme)

			if transportValid {
				plain, secure := "ws", "wss"
				if normalized.Transport == OpAMPTransportHTTP {
					plain, secure = "http", "https"
				}
				if scheme != plain && scheme != secure {
					errs = append(errs, fmt.Errorf("opamp.endpoint: scheme %q does not match transport %s (expected %s:// or %s://)", scheme, normalized.Transport, plain, secure))
				}
			}

			if scheme == "ws" || scheme == "http" {
				if !normalized.PlaintextAllowed() {
					errs = append(errs, fmt.Errorf("opamp.endpoint: %s:// sends the token in clear; use wss:// or https://, or point it at a loopback address, or set opamp.tls.insecure_skip_verify: true explicitly", scheme))
				}
			}
		}
	}

	if _, err := normalized.Heartbeat(); err != nil {
		errs = append(errs, err)
	}

	if normalized.TLS.CAFile != "" {
		pemData, err := os.ReadFile(normalized.TLS.CAFile)
		if err != nil {
			errs = append(errs, fmt.Errorf("opamp.tls.ca_file %q: %w", normalized.TLS.CAFile, err))
		} else {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pemData) {
				errs = append(errs, fmt.Errorf("opamp.tls.ca_file %q: no certificate parsed", normalized.TLS.CAFile))
			}
		}
	}

	return errs
}

// RedactEndpoint returns an endpoint with userinfo masked and query/fragment
// removed.
func RedactEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "(unparseable endpoint)"
	}

	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""

	return u.Redacted()
}
