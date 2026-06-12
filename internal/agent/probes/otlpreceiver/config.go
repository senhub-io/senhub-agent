package otlpreceiver

import (
	"fmt"

	"senhub-agent.go/internal/agent/probes/types"
)

const (
	defaultProtocol = "grpc"
	// Listen defaults are loopback-only: the receiver has no
	// authentication, so accepting remote OTLP requires an explicit
	// `address` opt-in (#278). Matches the upstream OTel collector,
	// which made the same default flip in v0.104.
	defaultGRPCAddr  = "127.0.0.1:4317"
	defaultHTTPAddr  = "127.0.0.1:4318"
	defaultHTTPPath  = "/v1/metrics"
	minPort          = 1
	maxPort          = 65535
	protocolGRPC     = "grpc"
	protocolHTTP     = "http"
	maxRecvMsgBytes  = 4 * 1024 * 1024 // 4 MiB — matches the OTel SDK default
	httpReadTimeoutS = 30
)

// receiverConfig is the parsed, validated configuration of the OTLP
// receiver probe.
type receiverConfig struct {
	// Protocol selects the listener transport: "grpc" (OTLP/gRPC,
	// default) or "http" (OTLP/HTTP protobuf).
	Protocol string
	// Address is the listen address (host:port). Defaults depend on
	// the protocol (4317 for gRPC, 4318 for HTTP) so a bare config
	// matches the OTLP well-known ports.
	Address string
	// HTTPPath is the route the HTTP receiver serves metrics on.
	// Ignored for gRPC.
	HTTPPath string
}

func parseReceiverConfig(config map[string]interface{}) (receiverConfig, error) {
	cfg := receiverConfig{
		Protocol: defaultProtocol,
		HTTPPath: defaultHTTPPath,
	}

	if v, ok := config["protocol"].(string); ok && v != "" {
		cfg.Protocol = v
	}
	if cfg.Protocol != protocolGRPC && cfg.Protocol != protocolHTTP {
		return receiverConfig{}, fmt.Errorf("protocol must be %q or %q, got %q", protocolGRPC, protocolHTTP, cfg.Protocol)
	}

	if v, ok := config["address"].(string); ok && v != "" {
		cfg.Address = v
	} else if cfg.Protocol == protocolHTTP {
		cfg.Address = defaultHTTPAddr
	} else {
		cfg.Address = defaultGRPCAddr
	}

	if v, ok := config["http_path"].(string); ok && v != "" {
		cfg.HTTPPath = v
	}

	// An explicit port override keeps the configured host but replaces
	// the port — convenience for operators who only want to move the
	// port off the default.
	if port, ok := types.IntParam(config, "port"); ok {
		if port < minPort || port > maxPort {
			return receiverConfig{}, fmt.Errorf("port must be between %d and %d, got %d", minPort, maxPort, port)
		}
		cfg.Address = replacePort(cfg.Address, port)
	}

	return cfg, nil
}
