package windowseventlog

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "windows_eventlog",
		DisplayName:     "Windows Event Log",
		Category:        "logs",
		Summary:         "Subscribes to Windows Event Log channels and emits events as logs (Windows only).",
		DocsPath:        "docs/user-guide/docs/probes/windows-eventlog.md",
		MultiInstance:   true,
		Platforms:       []string{"windows"},
		DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "channels", Kind: probes.KindStringList, Required: true, Group: "source", Description: "Channel names", Example: "System, Security"},
			{Key: "levels", Kind: probes.KindStringList, Group: "filter", Enum: []string{"Critical", "Error", "Warning", "Information", "Verbose"}, Description: "Levels to keep; empty means all"},
			{Key: "include_event_ids", Kind: probes.KindStringList, Group: "filter", Description: "Event IDs to keep; empty means all"},
			{Key: "exclude_event_ids", Kind: probes.KindStringList, Group: "filter", Description: "Event IDs to drop; wins over the include list"},
			{Key: "sources", Kind: probes.KindStringList, Group: "filter", Description: "Provider name patterns", Example: "Citrix*"},
			{Key: "bookmark_path", Kind: probes.KindString, Group: "source", Description: "File persisting the subscription position; use a distinct one per instance"},
			{Key: "backlog", Kind: probes.KindBool, Default: false, Group: "source", Description: "Replay events from the bookmark, or the whole channel without one, before tailing live"},
			{Key: "redact_pii", Kind: probes.KindBool, Default: false, Group: "privacy", Description: "Blank account names and addresses in Security events"},
			{Key: "poll_interval", Kind: probes.KindDuration, Default: "30s", Group: "collection", Description: "Bookmark flush cadence; delivery itself is push-based"},
		},
	})
}
