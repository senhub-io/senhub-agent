package opamp

import (
	"github.com/open-telemetry/opamp-go/protobufs"

	"senhub-agent.go/internal/agent/services/configuration"
)

const effectiveConfigNotice = 1 << 20

func buildEffectiveConfig(files []configuration.RedactedFile) *protobufs.EffectiveConfig {
	configMap := make(map[string]*protobufs.AgentConfigObject, len(files))
	for _, file := range files {
		configMap[file.Name] = &protobufs.AgentConfigObject{
			Body:        file.Body,
			ContentType: "text/yaml",
		}
	}

	return &protobufs.EffectiveConfig{
		ConfigMap: &protobufs.AgentConfigMap{
			ConfigMap: configMap,
		},
	}
}

func effectiveConfigSize(ec *protobufs.EffectiveConfig) int {
	if ec == nil || ec.GetConfigMap() == nil {
		return 0
	}

	size := 0
	for _, file := range ec.GetConfigMap().GetConfigMap() {
		if file != nil {
			size += len(file.GetBody())
		}
	}
	return size
}
