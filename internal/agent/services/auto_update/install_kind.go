package auto_update

import (
	"os"
	"runtime"
	"sync"
)

// Install kinds reported as senhub.install.kind.
const (
	KindMSI     = "msi"
	KindZip     = "zip"
	KindDeb     = "deb"
	KindRPM     = "rpm"
	KindTarball = "tarball"
	KindUnknown = "unknown"
)

var (
	installKindOnce  sync.Once
	installKindValue string
	// detectInstallKindFn is replaced by tests.
	detectInstallKindFn = detectInstallKind
)

// InstallKind says how this agent was installed. It is computed once: the
// answer cannot change while the process runs.
func InstallKind() string {
	installKindOnce.Do(func() { installKindValue = detectInstallKindFn() })
	return installKindValue
}

// detectInstallKind reads the evidence the installers leave:
//
//   - msi: the registry marker the MSI writes (isMsiManaged);
//   - zip: any other Windows install;
//   - deb, rpm: the running binary belongs to a package, known to dpkg
//     through its file lists or to rpm through its database (PackageOwner),
//     which is the case of the /usr/bin/senhub-agent the packages install;
//   - tarball: a Linux binary no package owns.
//
// Anything else, including a binary whose path cannot be read, is unknown.
func detectInstallKind() string {
	return installKindFor(runtime.GOOS, isMsiManaged(), executablePath(), PackageOwner)
}

func executablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

func installKindFor(goos string, msi bool, exe string, owner func(exe string) string) string {
	switch {
	case msi:
		return KindMSI
	case goos == "windows":
		return KindZip
	case goos != "linux" || exe == "":
		return KindUnknown
	}
	switch owner(exe) {
	case "deb":
		return KindDeb
	case "rpm":
		return KindRPM
	}
	return KindTarball
}
