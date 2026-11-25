package agentstate

import "sync"

// Where the update setting in force comes from.
const (
	UpdateSourceLocal  = "local"
	UpdateSourceServer = "server"
)

// UpdateSetting is the auto-update configuration in force, as the updater
// publishes it: a server directive overrides the local file, and a pause
// suspends the periodic check without changing anything else.
type UpdateSetting struct {
	Enabled     bool   // the automatic update is on
	IncludeBeta bool   // "latest" resolves on the beta channel
	Target      string // "latest" or a pinned version (X.Y.Z or X.Y.Z-beta.N)
	Source      string // "local" (agent.yaml) or "server" (a directive pushed by a server is in force)
	Paused      bool   // the periodic check is suspended by an extension
}

var updateSetting = struct {
	mu      sync.RWMutex
	setting *UpdateSetting
}{}

// SetUpdateSetting publishes the setting in force. The value is stored
// privately so a caller cannot mutate the published state through the copy
// it passed.
func SetUpdateSetting(s UpdateSetting) {
	updateSetting.mu.Lock()
	updateSetting.setting = &s
	updateSetting.mu.Unlock()
}

// GetUpdateSetting returns a copy of the published setting; ok is false
// until the updater has published once.
func GetUpdateSetting() (UpdateSetting, bool) {
	updateSetting.mu.RLock()
	defer updateSetting.mu.RUnlock()
	if updateSetting.setting == nil {
		return UpdateSetting{}, false
	}
	return *updateSetting.setting, true
}

// ResetUpdateSettingForTest forgets the published setting. Tests only.
func ResetUpdateSettingForTest() {
	updateSetting.mu.Lock()
	updateSetting.setting = nil
	updateSetting.mu.Unlock()
}
