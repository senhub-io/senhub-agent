package otlpreceiver

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:          "otlp_receiver",
		DisplayName:   "OTLP Receiver",
		Category:      "integration",
		Summary:       "Listens for OTLP metrics, logs and traces pushed by applications (gRPC or HTTP) and routes them like local data; one instance per listener.",
		DocsPath:      "docs/user-guide/docs/probes/otlp-receiver.md",
		MultiInstance: true,
		Params: []probes.ParamSpec{
			{Key: "protocol", Kind: probes.KindString, Default: "grpc", Enum: []string{"grpc", "http"}, Essential: true, Group: "connection", Description: "Listener transport: OTLP/gRPC or OTLP/HTTP protobuf"},
			{Key: "address", Kind: probes.KindString, Essential: true, Group: "connection", Description: "Listen address (host:port); 127.0.0.1:4317 for grpc and 127.0.0.1:4318 for http when empty, so remote senders need an explicit address", Example: "0.0.0.0:4317"},
			{Key: "port", Kind: probes.KindInt, Group: "connection", Description: "Replaces only the port part of the address"},
			{Key: "http_path", Kind: probes.KindString, Default: "/v1/metrics", Group: "connection", Description: "Route the HTTP receiver serves metrics on; logs and traces keep /v1/logs and /v1/traces; ignored for grpc"},
			{Key: "signals", Kind: probes.KindStringList, Default: []string{"metrics"}, Enum: []string{"metrics", "logs", "traces"}, Essential: true, Group: "collection", Description: "Signals the listener accepts; empty means metrics only"},
			{Key: "bearer_token", Kind: probes.KindString, Secret: true, Group: "auth", Description: "Token senders must present as Authorization: Bearer; empty accepts unauthenticated senders"},
			{Key: "allowed_cidrs", Kind: probes.KindStringList, Group: "auth", Description: "Source ranges (CIDR) allowed to send, checked on the transport peer address; empty allows any", Example: "10.0.0.0/8"},
			{Key: "rate_limit_rps", Kind: probes.KindFloat, Default: 0, Group: "advanced", Description: "Accepted requests per second; 0 turns rate limiting off"},
			{Key: "rate_limit_burst", Kind: probes.KindInt, Group: "advanced", Description: "Token bucket burst; twice rate_limit_rps when empty, needs rate_limit_rps"},
		},
	})
}
