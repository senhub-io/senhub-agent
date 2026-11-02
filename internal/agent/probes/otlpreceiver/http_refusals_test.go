package otlpreceiver

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

func postWithContentType(t *testing.T, url, contentType string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	return resp
}

func newTestProbeWithLogger(t *testing.T, cfg map[string]interface{}, cb *captureCallback, lg *logger.Logger) (*OTLPReceiverProbe, chan struct{}) {
	t.Helper()
	p, err := NewOTLPReceiverProbe(cfg, lg)
	if err != nil {
		t.Fatalf("NewOTLPReceiverProbe: %v", err)
	}
	probe := p.(*OTLPReceiverProbe)
	probe.SetName("otlp_receiver_test")
	probe.SetCallback(cb.fn)

	quit := make(chan struct{})
	if err := probe.OnStart(quit); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() {
		close(quit)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = probe.OnShutdown(ctx)
	})
	return probe, quit
}

func refusedCount(signal, reason string) uint64 {
	for k, v := range agentstate.GetOTLPReceiverRefusedBySignal() {
		if k.Signal == signal && k.Reason == reason {
			return v
		}
	}
	return 0
}

func TestHTTPReceiver_AcceptsProtobufWithCharsetParameter(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	body, err := proto.Marshal(sampleRequest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath
	resp := postWithContentType(t, url, "application/x-protobuf; charset=utf-8", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	waitForPoints(t, cb, 2)
}

func TestHTTPReceiver_AcceptsJSONMetrics(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	body, err := protojson.Marshal(sampleRequest())
	if err != nil {
		t.Fatalf("protojson marshal: %v", err)
	}
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath
	resp := postWithContentType(t, url, "application/json", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("response Content-Type = %q, want application/json", ct)
	}
	waitForPoints(t, cb, 2)
}

func TestHTTPReceiver_TextPlainAnswers415AndCountsMediaType(t *testing.T) {
	agentstate.ResetOTLPReceiverCountersForTest()
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath

	resp := postWithContentType(t, url, "text/plain", []byte("hello"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
	if got := refusedCount(signalMetrics, "media_type"); got != 1 {
		t.Errorf("refused[metrics,media_type] = %d, want 1", got)
	}
}

func TestHTTPReceiver_OversizeAnswers413AndCountsTooLarge(t *testing.T) {
	agentstate.ResetOTLPReceiverCountersForTest()
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath

	resp := postWithContentType(t, url, "application/x-protobuf", make([]byte, maxRecvMsgBytes+1))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
	if got := refusedCount(signalMetrics, "too_large"); got != 1 {
		t.Errorf("refused[metrics,too_large] = %d, want 1", got)
	}
}

func TestHTTPReceiver_RefusalLogIsRateLimitedPerSignalAndReason(t *testing.T) {
	agentstate.ResetOTLPReceiverCountersForTest()
	buf := &lockedBuffer{}
	zlog := zerolog.New(buf)
	lg := (*logger.Logger)(&zlog)

	cb := &captureCallback{}
	probe, _ := newTestProbeWithLogger(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb, lg)
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath

	now := time.Now()
	probe.refusals = newRefusalLogState(func() time.Time { return now })

	post := func() {
		resp := postWithContentType(t, url, "text/plain", []byte("x"))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d, want 415", resp.StatusCode)
		}
	}

	post()
	post()
	if lines := strings.Count(buf.String(), `"reason":"media_type"`); lines != 1 {
		t.Fatalf("log lines for media_type = %d, want 1 (rate-limited)", lines)
	}
	if got := refusedCount(signalMetrics, "media_type"); got != 2 {
		t.Fatalf("refused[metrics,media_type] = %d, want 2 (counting is not rate-limited)", got)
	}

	now = now.Add(2 * time.Minute)
	post()
	out := buf.String()
	if lines := strings.Count(out, `"reason":"media_type"`); lines != 2 {
		t.Fatalf("log lines for media_type after the window = %d, want 2", lines)
	}
	if !strings.Contains(out, `"suppressed":1`) {
		t.Errorf("expected the second line to carry suppressed=1, got: %s", out)
	}
}

// lockedBuffer is a log sink the server goroutines and the test can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
