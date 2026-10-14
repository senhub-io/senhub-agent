package entity

import "fmt"

// ListenerEndpoint is the service.endpoint identity of a socket listening on
// this host. hostsvc mints listeners with it and active checks aim at them
// with it: two spellings of one identity drift apart, and the day they do a
// check points at a listener nothing else names.
func ListenerEndpoint(hostID string, port int, transport string) string {
	return fmt.Sprintf("%s:%d/%s", hostID, port, transport)
}
