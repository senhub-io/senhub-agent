//go:build !linux

package app

import "os"

func platformInstallProbe() installProbe {
	return installProbe{
		logDirAccess: hostLogDirAccess,
		serviceBin:   installedServiceBinary,
		binVersion:   binaryVersion,
		pathBinaries: binariesOnPath,
	}
}

// platformServiceEnv reads the Windows service's registry environment
// the way config check does.
func platformServiceEnv(configPath string) func() serviceEnvReport {
	return func() serviceEnvReport {
		env, err := serviceEnvironment(configPath)
		rep := serviceEnvReport{}
		if err != nil {
			rep.Unreadable = []string{err.Error()}
		}
		for k, v := range env {
			if _, set := os.LookupEnv(k); !set && os.Setenv(k, v) == nil {
				rep.Applied++
			}
		}
		return rep
	}
}
