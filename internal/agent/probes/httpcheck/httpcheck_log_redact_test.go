package httpcheck

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestProbeLogLinesMaskTargetSecrets(t *testing.T) {
	const target = "http://lab:S3CRETpw@127.0.0.1:1/p?api_key=QRYsecret&x=QRYother#FRAGsecret"

	var buf bytes.Buffer
	base := zerolog.New(logger.NewMaskingWriter(&buf)).Level(zerolog.DebugLevel)
	probe, err := NewHTTPCheckProbe(map[string]interface{}{"targets": []interface{}{target}}, &base)
	if err != nil {
		t.Fatalf("NewHTTPCheckProbe: %v", err)
	}
	p := probe.(*HTTPCheckProbe)
	p.SetName("web-edge")

	if err := p.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	out := buf.String()
	for _, msg := range []string{"Starting http_check probe", "http_check target failed"} {
		if !strings.Contains(out, msg) {
			t.Fatalf("log line %q missing:\n%s", msg, out)
		}
	}
	for _, leak := range []string{"S3CRETpw", "lab:", "QRYsecret", "QRYother", "FRAGsecret"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q:\n%s", leak, out)
		}
	}
}
