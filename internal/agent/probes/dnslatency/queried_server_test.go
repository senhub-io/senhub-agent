package dnslatency

import (
	"net"
	"strings"
	"testing"
)

func TestAFailedLookupNamesTheResolverAsked(t *testing.T) {
	err := withQueriedServer(&net.DNSError{Err: "no such host", Name: "www.lab.", Server: "127.0.0.53:53", IsNotFound: true}, "10.10.0.12:53")
	if !strings.Contains(err.Error(), "on 10.10.0.12:53") || strings.Contains(err.Error(), "127.0.0.53") {
		t.Errorf("error names the wrong server: %v", err)
	}

	sys := withQueriedServer(&net.DNSError{Err: "no such host", Name: "x.", Server: "127.0.0.53:53"}, systemResolverLabel)
	if !strings.Contains(sys.Error(), "127.0.0.53") {
		t.Errorf("the system resolver's own answer must keep its server: %v", sys)
	}
}
