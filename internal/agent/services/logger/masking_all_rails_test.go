package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/cliArgs"
)

// TestMaskingCoversEveryRail builds the logger the way the production and
// development builders do, one MaskingWriter in front of the fan-out, and
// checks that the console, the file and the self-log rail all receive the
// same masked record for a URL carrying every secret form at once.
func TestMaskingCoversEveryRail(t *testing.T) {
	const target = "https://user:s3cretPW@h.example.com/x?x=plainVAL&api_key=tok3nVAL#fr4gSEC"
	const errText = "checking " + target + ": Get \"" + target + "\": dial tcp 10.0.0.1:443: connect: connection refused"

	var console, file bytes.Buffer
	sink := &recordSink{}
	self := newSelfLogWriterWithClock(zerolog.WarnLevel, func() time.Time { return time.Unix(1700000000, 0) })
	self.publish = sink.publish

	fanOut := zerolog.MultiLevelWriter(zerolog.ConsoleWriter{Out: &console, NoColor: true}, &file, self)
	log := zerolog.New(NewMaskingWriter(fanOut)).With().Timestamp().Str("module", "probe.http_check").Logger()
	log.Warn().Str("target", target).Str("error", errText).Msg("http_check target failed")

	recs := sink.all()
	if len(recs) != 1 {
		t.Fatalf("self-log rail published %d records, want 1", len(recs))
	}
	var selfOut strings.Builder
	selfOut.WriteString(recs[0].Body)
	for k, v := range recs[0].Attributes {
		selfOut.WriteString(" " + k + "=" + v)
	}

	rails := map[string]string{"console": console.String(), "file": file.String(), "otlp": selfOut.String()}
	leaks := []string{"s3cretPW", "user:", "user@", "x=plainVAL", "plainVAL", "tok3nVAL", "fr4gSEC"}
	for name, out := range rails {
		if out == "" {
			t.Fatalf("%s rail received nothing", name)
		}
		for _, leak := range leaks {
			if strings.Contains(out, leak) {
				t.Errorf("%s rail leaks %q:\n%s", name, leak, out)
			}
		}
		if !strings.Contains(out, "h.example.com") {
			t.Errorf("%s rail lost the host name:\n%s", name, out)
		}
	}

	// The file rail is NDJSON: one line, still valid JSON, colon kept after
	// the masked fragment.
	line := strings.TrimSuffix(file.String(), "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("file rail wrote more than one line: %q", line)
	}
	var evt map[string]any
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		t.Fatalf("file rail is not JSON: %v\n%s", err, line)
	}
	errField, _ := evt["error"].(string)
	if !strings.Contains(errField, "#***: Get") {
		t.Errorf("punctuation after the masked fragment was lost: %q", errField)
	}
	if tgt, _ := evt["target"].(string); !strings.HasSuffix(tgt, "#***") {
		t.Errorf("target not masked up to its fragment: %q", tgt)
	}
}

// TestMaskingWriterKeepsLevelForLevelWriters checks that a level-aware
// output behind the MaskingWriter still sees the record's level, which the
// self-log rail needs to drop everything under its threshold.
func TestMaskingWriterKeepsLevelForLevelWriters(t *testing.T) {
	sink := &recordSink{}
	self := newSelfLogWriterWithClock(zerolog.WarnLevel, func() time.Time { return time.Unix(1700000000, 0) })
	self.publish = sink.publish
	log := zerolog.New(NewMaskingWriter(zerolog.MultiLevelWriter(self)))
	log.Info().Msg("below threshold")
	log.Warn().Msg("at threshold")
	recs := sink.all()
	if len(recs) != 1 || recs[0].Body != "at threshold" {
		t.Fatalf("level not carried through the masking writer: %+v", recs)
	}
}

// TestBuilderMasksTheSelfLogRail goes through the real logger builder: the
// shipper and the self-log rail must receive the masked record, whatever
// the builder puts in front of them.
func TestBuilderMasksTheSelfLogRail(t *testing.T) {
	const target = "https://user:s3cretPW@h.example.com/x?x=plainVAL#fr4gSEC"
	var shipper bytes.Buffer
	sink := &recordSink{}
	self := newSelfLogWriterWithClock(zerolog.WarnLevel, func() time.Time { return time.Unix(1700000000, 0) })
	self.publish = sink.publish

	log := buildDevelopmentLogger(&cliArgs.ParsedArgs{}, &LoggerConfig{logShipper: &shipper}, self)
	log.Warn().Str("module", "probe.http_check").Str("target", target).Msg("checking " + target + ": connection refused")

	recs := sink.all()
	if len(recs) != 1 {
		t.Fatalf("self-log rail published %d records, want 1", len(recs))
	}
	out := recs[0].Body
	for k, v := range recs[0].Attributes {
		out += " " + k + "=" + v
	}
	for name, text := range map[string]string{"otlp": out, "shipper": shipper.String()} {
		for _, leak := range []string{"s3cretPW", "user:", "user@", "plainVAL", "fr4gSEC"} {
			if strings.Contains(text, leak) {
				t.Errorf("%s rail leaks %q through the builder:\n%s", name, leak, text)
			}
		}
	}
}
