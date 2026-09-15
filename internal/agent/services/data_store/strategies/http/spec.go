package http

import (
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

func init() {
	outputspec.Register(outputspec.Output{
		Type: "http", DisplayName: "Console and pull endpoints", Mode: outputspec.ModePull, Singleton: true,
		Summary:  "Serves this console and the PRTG, Nagios and Prometheus endpoints a poller reads.",
		DocsPath: "docs/user-guide/docs/http-https.md",
		Params: []spec.ParamSpec{
			{Key: "port", Kind: spec.KindInt, Default: 8080, Essential: true, Group: "listen", Description: "TCP port of the console and the pull endpoints"},
			{Key: "bind_address", Kind: spec.KindString, Default: "127.0.0.1", Essential: true, Group: "listen", Description: "127.0.0.1 for this machine only, 0.0.0.0 for other machines"},
			{Key: "endpoints", Kind: spec.KindStringList, Enum: []string{"prtg", "nagios", "prometheus", "web"}, Essential: true, Group: "listen", Description: "Endpoint families served; web is this console"},
			{Key: "tls", Kind: spec.KindBlock, Group: "tls", Description: "Serve HTTPS", Fields: []spec.ParamSpec{
				{Key: "enabled", Kind: spec.KindBool, Default: false},
				{Key: "min_tls_version", Kind: spec.KindString, Default: "1.2", Enum: []string{"1.2", "1.3"}},
				{Key: "cert_file", Kind: spec.KindString, Description: "Server certificate (PEM)"},
				{Key: "key_file", Kind: spec.KindString, Description: "Server key (PEM)"},
			}},
			{Key: "max_cache_size", Kind: spec.KindInt, Group: "cache", Description: "Cap on cached metrics; the oldest are dropped past it"},
			{Key: "prometheus", Kind: spec.KindBlock, Group: "prometheus", Fields: []spec.ParamSpec{
				{Key: "include_probe_tags", Kind: spec.KindBool, Default: true, Description: "Emit probe tags as labels"},
				{Key: "expose_host_metrics", Kind: spec.KindBool, Default: true, Description: "Include the host probes in /metrics"},
			}},
		},
	})
}
