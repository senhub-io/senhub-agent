package kuberneteslogs

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:          "kubernetes_logs",
		DisplayName:   "Kubernetes Logs",
		Category:      "logs",
		Summary:       "Ships stdout/stderr pod logs from the current Kubernetes node.",
		DocsPath:      "docs/user-guide/docs/probes/kubernetes_logs.md",
		MultiInstance: false,
		Params: []probes.ParamSpec{
			{Key: "root", Kind: probes.KindString, Default: "/var/log/pods", Group: "source", Description: "Root directory containing the pod log directory structure"},
			{Key: "include_namespaces", Kind: probes.KindStringList, Group: "filter", Description: "Follow only pods in namespaces matching one of these glob patterns"},
			{Key: "exclude_namespaces", Kind: probes.KindStringList, Group: "filter", Description: "Never follow pods in namespaces matching one of these glob patterns"},
			{Key: "include_pods", Kind: probes.KindStringList, Group: "filter", Description: "Follow only pods whose name matches one of these glob patterns"},
			{Key: "exclude_pods", Kind: probes.KindStringList, Group: "filter", Description: "Never follow pods whose name matches one of these glob patterns"},
			{Key: "scan_interval", Kind: probes.KindString, Default: "5s", Group: "source", Description: "How often the node log directory is rescanned"},
			{Key: "from_beginning", Kind: probes.KindBool, Default: false, Group: "source", Description: "Read existing content the first time a file is seen"},
			{Key: "max_line_bytes", Kind: probes.KindInt, Default: 262144, Group: "advanced", Description: "Cap on one CRI message after partial reassembly, in bytes"},
			{Key: "bookmark_path", Kind: probes.KindString, Group: "source", Description: "File persisting read offsets across restarts; defaults to a probe-name bookmark file in the agent state directory"},
			{Key: "multiline", Kind: probes.KindBlock, Group: "parsing", Description: "Fold continuation lines (stack traces) into one record, per container stream", Fields: []probes.ParamSpec{
				{Key: "pattern", Kind: probes.KindString, Description: "Regular expression tested against each message", Example: `^\d{4}-\d{2}-\d{2}`},
				{Key: "negate", Kind: probes.KindBool, Default: false, Description: "Invert the pattern match"},
				{Key: "match", Kind: probes.KindString, Default: "after", Enum: []string{"after", "before"}, Description: "Whether a matching line starts a record or flushes the previous one"},
			}},
			{Key: "parser", Kind: probes.KindBlock, Group: "parsing", Description: "Structured parsing of each CRI message", Fields: []probes.ParamSpec{
				{Key: "type", Kind: probes.KindString, Default: "raw", Enum: []string{"raw", "regex", "json", "logfmt"}, Description: "Message format"},
				{Key: "pattern", Kind: probes.KindString, Description: "Regular expression with named groups; required for the regex type"},
				{Key: "timestamp_field", Kind: probes.KindString, Description: "Field carrying the message timestamp"},
				{Key: "timestamp_format", Kind: probes.KindString, Description: "Go reference-time layout of that field", Example: "2006-01-02 15:04:05"},
			}},
		},
	})
}
