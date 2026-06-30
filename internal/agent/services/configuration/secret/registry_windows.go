//go:build windows

package secret

// InitRegistry installs the Windows DPAPI-backed provider as the active secret
// backend. configDir is where the entropy and ciphertext store live.
func InitRegistry(configDir string) error {
	p, err := NewDPAPIProvider(configDir)
	if err != nil {
		return err
	}
	SetProvider(p)
	return nil
}
