//go:build !linux

package app

func platformInstallProbe() installProbe {
	return installProbe{
		logDirAccess: hostLogDirAccess,
		serviceBin:   installedServiceBinary,
		binVersion:   binaryVersion,
	}
}
