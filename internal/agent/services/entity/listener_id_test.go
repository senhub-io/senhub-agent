package entity

import "testing"

func TestListenerEndpoint(t *testing.T) {
	if got, want := ListenerEndpoint("h1", 8080, "tcp"), "h1:8080/tcp"; got != want {
		t.Errorf("ListenerEndpoint = %q, want %q", got, want)
	}
}
