package senhub

import (
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

func init() {
	outputspec.Register(outputspec.Output{
		Type: "senhub", DisplayName: "SenHub cloud", Mode: outputspec.ModePush,
		Summary:  "Pushes metrics to the SenHub intake, authenticated by the agent key.",
		DocsPath: "docs/user-guide/docs/configuration.md",
		Params: []spec.ParamSpec{
			{Key: "interval", Kind: spec.KindDuration, Default: "5s", Essential: true, Group: "delivery", Description: "Push cadence"},
		},
	})
}
