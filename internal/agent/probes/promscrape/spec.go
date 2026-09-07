package promscrape

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "prometheus_scrape", DisplayName: "Prometheus Scrape", Category: "integration",
		Summary:  "Scrapes Prometheus exposition endpoints and relays their metrics.",
		DocsPath: "docs/user-guide/docs/probes/prometheus-scrape.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "targets", Kind: probes.KindStringList, Required: true, Group: "targets", Description: "Exposition URLs", Example: "http://localhost:9100/metrics"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between scrapes"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Whole-request budget in seconds"},
			{Key: "metric_match", Kind: probes.KindString, Group: "filter", Description: "Regular expression on metric family names"},
			{Key: "bearer_token", Kind: probes.KindString, Secret: true, Group: "auth", Description: "Sent as Authorization: Bearer"},
			{Key: "insecure_skip_verify", Kind: probes.KindBool, Default: false, Group: "tls", Description: "Accept self-signed exporter certificates"},
		},
	})
}
