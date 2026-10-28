package otlpreceiver

import (
	"bytes"
	"compress/gzip"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func postEncoded(t *testing.T, url, encoding string, body []byte) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestHTTPReceiver_AcceptsGzipMetrics(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	body, err := proto.Marshal(sampleRequest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath
	if code := postEncoded(t, url, "gzip", gzipBytes(t, body)); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	waitForPoints(t, cb, 2)
}

func TestHTTPReceiver_AcceptsIdentityEncoding(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	body, err := proto.Marshal(sampleRequest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath
	if code := postEncoded(t, url, "identity", body); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	waitForPoints(t, cb, 2)
}

func TestHTTPReceiver_AcceptsGzipLogs(t *testing.T) {
	sub := agentstate.SubscribeLogs(16)
	defer agentstate.UnsubscribeLogs(sub)

	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{
		"protocol": "http", "address": "127.0.0.1:0",
		"signals": []interface{}{"logs"},
	}, cb)
	rec := &logspb.LogRecord{
		Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "gzipped"}},
	}
	body, err := proto.Marshal(&collectorlogspb.ExportLogsServiceRequest{ResourceLogs: wrapLogs(nil, rec)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	url := "http://" + probe.listener.Addr().String() + httpLogsPath
	if code := postEncoded(t, url, "gzip", gzipBytes(t, body)); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	select {
	case got := <-sub:
		if got.Body != "gzipped" {
			t.Errorf("relayed body = %q, want gzipped", got.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the relayed log record")
	}
}

func TestHTTPReceiver_OversizeBodiesAnswer413(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath

	big := make([]byte, maxRecvMsgBytes+1)
	if code := postEncoded(t, url, "", big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize plain: status = %d, want 413", code)
	}
	if code := postEncoded(t, url, "gzip", gzipBytes(t, big)); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize decompressed: status = %d, want 413", code)
	}
}

func TestHTTPReceiver_BodyAtTheCapIsNotRejectedAs413(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath

	if code := postEncoded(t, url, "", make([]byte, maxRecvMsgBytes)); code == http.StatusRequestEntityTooLarge {
		t.Errorf("body of exactly the cap: status = %d, want not 413", code)
	}
}

func TestHTTPReceiver_UnknownEncodingAnswers415(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath

	if code := postEncoded(t, url, "br", []byte("x")); code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", code)
	}
}

func TestHTTPReceiver_CorruptGzipAnswers400(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath

	if code := postEncoded(t, url, "gzip", []byte("not gzip")); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestHTTPReceiver_IncompressibleGzipOverTheCapAnswers413(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	url := "http://" + probe.listener.Addr().String() + defaultHTTPPath

	noise := make([]byte, maxRecvMsgBytes+4096)
	rand.New(rand.NewSource(1)).Read(noise)
	if code := postEncoded(t, url, "gzip", gzipBytes(t, noise)); code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", code)
	}
}
