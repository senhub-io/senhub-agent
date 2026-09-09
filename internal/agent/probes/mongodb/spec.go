package mongodb

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "mongodb", DisplayName: "MongoDB", Category: "database",
		Summary:  "Connections, operations, memory, locks, per-database storage and replica set state of a MongoDB server; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/mongodb.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "uri", Kind: probes.KindString, Default: "mongodb://localhost:27017", Secret: true, Essential: true, Group: "connection", Description: "Connection URI; credentials go in it (mongodb://user:pass@host:27017/admin?authSource=admin)", Example: "mongodb://monitor:secret@db01:27017/admin?authSource=admin"},
			{Key: "direct_connection", Kind: probes.KindBool, Default: true, Group: "connection", Description: "Connect to the named host only; false for Atlas or replica-set aware routing"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Connection and command timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this server"},
		},
	})
}
