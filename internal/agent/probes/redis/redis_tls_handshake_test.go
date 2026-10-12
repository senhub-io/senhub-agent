package redis

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// plainRedis accepts connections and answers like a Redis without TLS: it
// reads whatever arrives and replies with an error line, which is not a
// TLS record.
func plainRedis(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 512)
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte("-ERR unknown command\r\n"))
			}()
		}
	}()
	return ln.Addr().String()
}

// A failing handshake is reported as one, with the target in the message,
// instead of as a failed AUTH (#585).
func TestCollect_TLSHandshakeFailureIsNamed(t *testing.T) {
	addr := plainRedis(t)
	host, port, _ := net.SplitHostPort(addr)

	p, err := NewRedisProbe(map[string]interface{}{
		"host": host, "port": port, "tls": true, "password": "secret", "timeout": "2s",
	}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewRedisProbe: %v", err)
	}
	rp := p.(*redisProbe)
	rp.SetName("redis-tls")
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	rp.moduleLogger = logger.NewModuleLogger((*logger.Logger)(&zl), "probe.redis.test")

	pts, err := rp.Collect()
	if err != nil {
		t.Fatalf("Collect returned an error: %v", err)
	}
	if up := indexDatapoints(pts)["senhub.db.up"]; len(up) == 0 || up[0].Value != 0 {
		t.Errorf("senhub.db.up must be 0 when the handshake fails, got %v", up)
	}
	out := buf.String()
	if !strings.Contains(out, "TLS handshake with "+addr+" failed") {
		t.Errorf("log lacks the handshake message for %s:\n%s", addr, out)
	}
	if strings.Contains(out, "AUTH send failed") {
		t.Errorf("a TLS failure must not surface as an AUTH failure:\n%s", out)
	}
}
