//go:build !windows

package app

// migrateLegacyServiceRegistration is a no-op outside Windows: the
// legacy command-line defect (#309) is specific to the Windows SCM
// ImagePath registration. The systemd unit is rewritten wholesale by
// `senhub-agent install`.
func migrateLegacyServiceRegistration() {}
