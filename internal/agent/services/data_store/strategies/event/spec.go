package event

import (
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

func init() {
	outputspec.Register(outputspec.Output{
		Type: "event", DisplayName: "Events (syslog and Windows events)", Mode: outputspec.ModePush,
		Summary:  "Pushes syslog and Windows event records to an event intake as batches.",
		DocsPath: "docs/user-guide/docs/configuration.md",
		Params: []spec.ParamSpec{
			{Key: "server_url", Kind: spec.KindString, Required: true, Group: "connection", Description: "Base URL of the event intake; /event/insert is appended", Example: "https://events.example.com"},
			{Key: "queue_size", Kind: spec.KindInt, Default: 1000, Group: "delivery", Description: "Records held before the oldest are dropped"},
			{Key: "sync_interval", Kind: spec.KindDuration, Default: "30s", Group: "delivery", Description: "Push cadence"},
		},
	})
}
