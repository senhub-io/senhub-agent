package phpfpm

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "phpfpm", DisplayName: "PHP-FPM", Category: "web",
		Summary:  "Worker processes, listen queue and slow requests of a PHP-FPM pool from its status page; one instance per pool.",
		DocsPath: "docs/user-guide/docs/probes/php-fpm.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost/fpm-status", Essential: true, Group: "connection", Description: "URL of the pool status page, answering in JSON"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this pool; set it when two phpfpm probes run on one host"},
		},
	})
}
