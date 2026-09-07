package httpcheck

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "http_check", DisplayName: "HTTP Check", Category: "web",
		Summary:  "Availability, status and latency of HTTP endpoints; one instance per check policy.",
		DocsPath: "docs/user-guide/docs/probes/http-check.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "targets", Kind: probes.KindStringList, Required: true, Group: "targets", Description: "URLs to check", Example: "https://app.example.com/health"},
			{Key: "method", Kind: probes.KindString, Default: "GET", Enum: []string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"}, Group: "request", Description: "HTTP method"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Whole-request budget in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between cycles"},
			{Key: "expected_status", Kind: probes.KindInt, Group: "request", Description: "Exact status that counts as up; empty means any 2xx or 3xx"},
			{Key: "content_match", Kind: probes.KindString, Group: "request", Description: "Regular expression the body must match", Example: "\"status\":\"ok\""},
			{Key: "insecure_skip_verify", Kind: probes.KindBool, Default: false, Group: "tls", Description: "Accept self-signed certificates"},
		},
	})
}
