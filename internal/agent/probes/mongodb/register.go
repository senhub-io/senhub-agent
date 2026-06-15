package mongodb

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("mongodb", NewMongoDBProbe) }
