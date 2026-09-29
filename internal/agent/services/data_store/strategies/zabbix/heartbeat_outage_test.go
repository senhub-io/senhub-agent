package zabbix

import (
	"context"
	"net"
	"testing"

	"senhub-agent.go/internal/agent/services/logger"
)

// A server that is down for a minute must not cost the heartbeat for the
// life of the agent: every error used to turn it off, and Zabbix then
// showed the host unavailable while its values kept arriving (beta 3
// retest, Zabbix 7 restarted).
func TestAnOutageDoesNotTurnTheHeartbeatOff(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	down := ln.Addr().String()
	_ = ln.Close() // nothing listens: connection refused

	c, _ := newClient(testConfig(down))
	s := &Strategy{client: c, logger: logger.NewModuleLogger(testLogger(), "test")}
	s.beat(context.Background())
	if s.heartbeatOff {
		t.Fatal("a refused connection turned the heartbeat off for good")
	}
}

// A server before 6.2 answers that it does not know the request: only
// that turns the heartbeat off.
func TestAnOldServerTurnsTheHeartbeatOff(t *testing.T) {
	srv := newFakeServer(t)
	srv.refuseInfo["active check heartbeat"] = "unknown request"
	c, _ := newClient(testConfig(srv.addr()))
	s := &Strategy{client: c, logger: logger.NewModuleLogger(testLogger(), "test")}
	s.beat(context.Background())
	if !s.heartbeatOff {
		t.Fatal("a server that does not know the heartbeat kept being asked")
	}
}
