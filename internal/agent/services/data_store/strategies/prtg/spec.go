package prtg

import (
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

func init() {
	outputspec.Register(outputspec.Output{
		Type: "prtg", DisplayName: "PRTG push", Mode: outputspec.ModePush,
		Summary:  "Pushes sensor results to a PRTG HTTP Push Data sensor, for hosts the PRTG probe cannot reach.",
		DocsPath: "docs/user-guide/docs/configuration.md",
		Params: []spec.ParamSpec{
			{Key: "server_url", Kind: spec.KindString, Required: true, Group: "connection", Description: "URL of the PRTG push sensor", Example: "https://prtg.example.com:5050/token"},
			{Key: "interval", Kind: spec.KindDuration, Default: "5s", Group: "delivery", Description: "Push cadence"},
			{Key: "data_retention_period", Kind: spec.KindDuration, Default: "2m", Group: "delivery", Description: "How long a value stays in the push buffer"},
		},
	})
}
