//go:build windows

package configuration

// dockerSocketAccess is a no-op on Windows: the Docker Engine is reached over
// a named pipe, not a Unix socket, and the swarm fragment is not written there.
func dockerSocketAccess(string) error {
	return nil
}
