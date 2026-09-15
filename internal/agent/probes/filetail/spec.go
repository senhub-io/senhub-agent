package filetail

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:          "filetail",
		DisplayName:   "File Tail",
		Category:      "logs",
		Summary:       "Tails log files and emits each record as a log; one instance per file set.",
		DocsPath:      "docs/user-guide/docs/probes/filetail.md",
		MultiInstance: true,
		Params: []probes.ParamSpec{
			{Key: "paths", Kind: probes.KindStringList, Required: true, Group: "source", Description: "File paths or glob patterns, re-expanded every 15 seconds", Example: "/var/log/app/*.log"},
			{Key: "bookmark_path", Kind: probes.KindString, Group: "source", Description: "File persisting read offsets across restarts; use a distinct one per instance", Example: "/var/lib/senhub-agent/filetail-app.json"},
			{Key: "from_beginning", Kind: probes.KindBool, Default: false, Group: "source", Description: "Read existing content the first time a file is seen"},
			{Key: "max_bytes_per_line", Kind: probes.KindInt, Default: 1048576, Group: "advanced", Description: "Cap on one record after multiline folding, in bytes"},
			{Key: "multiline", Kind: probes.KindBlock, Group: "parsing", Description: "Fold continuation lines into one record", Fields: []probes.ParamSpec{
				{Key: "pattern", Kind: probes.KindString, Description: "Regular expression tested against each line", Example: `^\d{4}-\d{2}-\d{2}`},
				{Key: "negate", Kind: probes.KindBool, Default: false, Description: "Invert the pattern match"},
				{Key: "match", Kind: probes.KindString, Default: "after", Enum: []string{"after", "before"}, Description: "Whether a matching line starts a record or flushes the previous one"},
			}},
			{Key: "parser", Kind: probes.KindBlock, Group: "parsing", Description: "Structured parsing of each record", Fields: []probes.ParamSpec{
				{Key: "type", Kind: probes.KindString, Default: "raw", Enum: []string{"raw", "regex", "json", "logfmt"}, Description: "Record format"},
				{Key: "pattern", Kind: probes.KindString, Description: "Regular expression with named groups; required for the regex type"},
				{Key: "timestamp_field", Kind: probes.KindString, Description: "Field carrying the record timestamp"},
				{Key: "timestamp_format", Kind: probes.KindString, Description: "Go reference-time layout of that field", Example: "2006-01-02 15:04:05"},
			}},
		},
	})
}
