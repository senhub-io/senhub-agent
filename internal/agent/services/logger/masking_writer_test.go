package logger

import (
	"bytes"
	"strings"
	"testing"
)

// TestMaskingWriter_PreservesNewlineBetweenEntries pins the
// regression that produced the bbcloud "log file is a single physical
// line" symptom. zerolog sends one entry per Write() call, payload
// terminated by '\n'. The MaskingWriter used to drop the newline
// when the payload was valid JSON (json.Marshal doesn't emit one).
// Result: every NDJSON consumer downstream — tail, jq, Loki, Splunk —
// saw a single blob and refused to parse it.
//
// The test feeds two valid JSON log entries back-to-back and asserts
// the output is line-delimited NDJSON (two lines, each one decodable
// as JSON on its own).
func TestMaskingWriter_PreservesNewlineBetweenEntries(t *testing.T) {
	var sink bytes.Buffer
	w := NewMaskingWriter(&sink)

	// zerolog-shaped inputs: each ends with '\n'.
	entries := []string{
		`{"level":"info","message":"hello","time":"2026-05-19T10:00:00Z"}` + "\n",
		`{"level":"warn","message":"world","time":"2026-05-19T10:00:01Z"}` + "\n",
	}
	for _, e := range entries {
		n, err := w.Write([]byte(e))
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if n != len(e) {
			t.Errorf("Write returned n=%d, want %d (caller may interpret as short-write)", n, len(e))
		}
	}

	got := sink.String()
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d:\n%s", len(lines), got)
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			t.Errorf("line %d not a standalone JSON object: %q", i, line)
		}
	}
}

func TestMaskingWriter_NonJSONInputAlsoNewlineTerminated(t *testing.T) {
	var sink bytes.Buffer
	w := NewMaskingWriter(&sink)

	// A bare line, no newline at the end — masking writer must
	// terminate it so downstream still gets one entry per line.
	plain := []byte("password=hunter2 user=admin")
	if _, err := w.Write(plain); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := sink.String()
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("non-JSON path must terminate output with '\\n'; got %q", got)
	}
	// Sanity: password value masked.
	if strings.Contains(got, "hunter2") {
		t.Errorf("non-JSON path didn't mask the password; got %q", got)
	}
}

func TestAppendNewlineIfMissing_Idempotent(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "\n"},
		{"hi", "hi\n"},
		{"hi\n", "hi\n"},
		{"a\nb", "a\nb\n"},
		{"a\nb\n", "a\nb\n"},
	}
	for _, tc := range cases {
		got := appendNewlineIfMissing([]byte(tc.in))
		if string(got) != tc.want {
			t.Errorf("appendNewlineIfMissing(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
