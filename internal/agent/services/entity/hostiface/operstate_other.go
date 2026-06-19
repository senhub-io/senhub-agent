//go:build !linux

package hostiface

// readSysLink has no portable non-Linux source for the carrier/type/duplex/
// speed metadata; the caller fills oper_state from the administrative IFF_UP
// flag and omits the rest.
func readSysLink(string) linkMeta { return linkMeta{} }
