package extension

import iextension "senhub-agent.go/internal/agent/services/extension"

// IsTargetParam reports whether a probe parameter says where the probe connects.
func IsTargetParam(key string) bool { return iextension.IsTargetParam(key) }
