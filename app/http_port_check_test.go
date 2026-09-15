package app

import (
	"net"
	"strings"
	"testing"
)

func TestCheckHTTPPortFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	err = checkHTTPPortFree("127.0.0.1", port)
	if err == nil {
		t.Fatalf("port %d is held by the test listener, the check must refuse it", port)
	}
	if !strings.Contains(err.Error(), "--http-port") {
		t.Errorf("the refusal must name the way out, got: %v", err)
	}

	ln.Close()
	if err := checkHTTPPortFree("127.0.0.1", port); err != nil {
		t.Errorf("port %d was released, the check must accept it: %v", port, err)
	}
}
