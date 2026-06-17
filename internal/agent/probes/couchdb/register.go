package couchdb

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("couchdb", NewCouchDBProbe) }
