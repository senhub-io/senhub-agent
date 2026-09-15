package logicaldisk

import (
	"strings"

	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// normalizeFSType lowercases the filesystem name the OS reports so the
// system.filesystem.type attribute carries the same form on every
// platform: Windows reports "NTFS"/"ReFS" while Linux mount tables are
// already lower-case (ext4/xfs) — cross-platform rules filter on the
// lower-case token (#627).
func normalizeFSType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// NewLogicalDiskProbe crée une nouvelle instance de logicaldisk probe. Le
// cycle de vie vient de hostpoll ; seule la collecte OS appartient à ce
// paquet.
func NewLogicalDiskProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	probe, err := hostpoll.New(config, baseLogger, hostpoll.Spec{
		Module:   "probe.logicaldisk",
		Subject:  "logicaldisk",
		TypeName: "logicaldiskProbe",
		NewCollector: func(cfg map[string]interface{}, base *logger.Logger, _ *logger.ModuleLogger) (hostpoll.Collector, error) {
			return newLogicalDiskCollector(cfg, base)
		},
	})
	if err != nil {
		return nil, err
	}
	return probe, nil
}
