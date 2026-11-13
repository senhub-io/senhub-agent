//go:build windows

package app

import "golang.org/x/sys/windows/registry"

// msiMarkerKey is the key the MSI writes (packaging/windows/senhub-agent.wxs);
// its Version value says the install is MSI-managed.
const msiMarkerKey = `SOFTWARE\Sensor Factory\SenHub Agent`

// registeredServiceImagePath returns the command line the SCM runs for the
// senhub-agent service, or "" when no such service is registered.
func registeredServiceImagePath() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, serviceRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	imagePath, _, err := key.GetStringValue("ImagePath")
	if err != nil {
		return ""
	}
	return imagePath
}

func hostMsiManaged() bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, msiMarkerKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	v, _, err := key.GetStringValue("Version")
	return err == nil && v != ""
}
