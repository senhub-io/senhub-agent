package couchdb

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "couchdb", DisplayName: "CouchDB", Category: "database",
		Summary:  "HTTP requests by method and status, database reads and writes, open databases and files of a CouchDB node; one instance per node.",
		DocsPath: "docs/user-guide/docs/probes/couchdb.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:5984", Essential: true, Group: "connection", Description: "Base URL of the node", Example: "http://couch01:5984"},
			{Key: "username", Kind: probes.KindString, Essential: true, Group: "auth", Description: "Admin user; the stats endpoint needs admin credentials by default"},
			{Key: "password", Kind: probes.KindString, Secret: true, Essential: true, Group: "auth", Description: "Admin user's password"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "HTTP request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this node"},
		},
	})
}
