package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestMaskingWriterRedactsURLsInJSONRecords(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	lines := []string{
		`{"level":"info","message":"Starting http_check probe","targets":["https://user:s3cretPW@h.example.com/x?token=tok3nVAL#fr4gSEC","https://clean.example.org/p"]}` + "\n",
		`{"level":"warn","message":"http_check target failed","error":"checking https://user:s3cretPW@h.example.com/x?api_key=tok3nVAL: dial tcp 10.0.0.1:443: connect: refused","jwt":"` + jwt + `","key":"AKIAFAKEFAKEFAKE1234"}` + "\n",
	}
	var buf bytes.Buffer
	w := NewMaskingWriter(&buf)
	for _, line := range lines {
		n, err := w.Write([]byte(line))
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
		if n != len(line) {
			t.Errorf("Write returned n=%d, want %d", n, len(line))
		}
	}
	out := buf.String()
	secrets := []string{"s3cretPW", "tok3nVAL", "fr4gSEC", jwt, "AKIAFAKEFAKEFAKE1234"}
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("output contains secret %q:\n%s", s, out)
		}
	}
	for _, keep := range []string{"https://clean.example.org/p", "h.example.com", "clean.example.org"} {
		if !strings.Contains(out, keep) {
			t.Errorf("output missing expected content %q:\n%s", keep, out)
		}
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("output must end with exactly one newline, got %q", out[len(out)-3:])
	}
}

func TestMaskingWriterRedactsNonJSONLine(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	line := "WRN http_check target failed error=\"checking https://user:s3cretPW@h.example.com/x?api_key=tok3nVAL: dial tcp ...\" jwt=" + jwt + " key=AKIAFAKEFAKEFAKE1234\n"
	var buf bytes.Buffer
	w := NewMaskingWriter(&buf)
	if _, err := w.Write([]byte(line)); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	out := buf.String()
	for _, s := range []string{"s3cretPW", "tok3nVAL", jwt, "AKIAFAKEFAKEFAKE1234"} {
		if strings.Contains(out, s) {
			t.Errorf("output contains secret %q:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "h.example.com") {
		t.Errorf("output missing host name:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("output must end with exactly one newline, got %q", out)
	}
}
