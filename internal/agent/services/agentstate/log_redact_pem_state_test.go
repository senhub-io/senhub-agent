package agentstate

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

func generatePEMKeyLines(t *testing.T) []string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		t.Fatalf("pem encode: %v", err)
	}
	lines := strings.Split(buf.String(), "\n")
	// Drop trailing empty line from final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func assertNoKeyLeak(t *testing.T, outputs []string, keyLines []string) {
	t.Helper()
	// keyLines: [BEGIN, body..., END]
	end := keyLines[len(keyLines)-1]
	bodyLines := keyLines[1 : len(keyLines)-1]

	for _, out := range outputs {
		for _, bodyLine := range bodyLines {
			if strings.Contains(out, bodyLine) {
				t.Fatalf("key body line %q leaked in output %q", bodyLine, out)
			}
		}
		if strings.Contains(out, end) && out != "***"+end {
			t.Fatalf("END marker leaked unmasked: %q", out)
		}
	}
}

func TestRedactPEMRealKeyOneRecordPerLine(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	keyLines := generatePEMKeyLines(t)
	outputs := []string{}
	probe := "file"
	attrs := map[string]string{"log.file.path": "/var/log/app.log"}

	for i, line := range keyLines {
		rec := LogRecord{ProducerProbeName: probe, Attributes: attrs, Body: line}
		out := redactLogRecord(rec)
		outputs = append(outputs, out.Body)
		if i == 0 {
			if out.Body != "-----BEGIN PRIVATE KEY-----***" {
				t.Fatalf("first record = %q, want masked BEGIN", out.Body)
			}
		} else if i < len(keyLines)-1 {
			if out.Body != "***" {
				t.Fatalf("body record %d = %q, want ***", i, out.Body)
			}
		} else {
			if out.Body != "***-----END PRIVATE KEY-----" {
				t.Fatalf("END record = %q, want masked END", out.Body)
			}
		}
	}

	assertNoKeyLeak(t, outputs, keyLines)
	if got := pemState.len(); got != 0 {
		t.Fatalf("pemState.len() = %d, want 0", got)
	}

	wantCount := uint64(len(keyLines))
	if got := privateKeyRedactionCount(t, probe); got != wantCount {
		t.Fatalf("redaction count = %d, want %d", got, wantCount)
	}
}

func TestRedactPEMRealKeyOrdinaryLineAfterBegin(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	keyLines := generatePEMKeyLines(t)
	probe := "file"
	attrs := map[string]string{"log.file.path": "/var/log/app.log"}

	// BEGIN
	rec := LogRecord{ProducerProbeName: probe, Attributes: attrs, Body: keyLines[0]}
	out := redactLogRecord(rec)
	if out.Body != "-----BEGIN PRIVATE KEY-----***" {
		t.Fatalf("BEGIN output = %q", out.Body)
	}

	// ordinary line
	rec.Body = "2026-10-26 12:00:00 INFO worker started"
	out = redactLogRecord(rec)
	if out.Body != "***" {
		t.Fatalf("ordinary line output = %q, want ***", out.Body)
	}

	// body lines and END
	for _, line := range keyLines[1:] {
		rec.Body = line
		out = redactLogRecord(rec)
		if line == keyLines[len(keyLines)-1] {
			if out.Body != "***-----END PRIVATE KEY-----" {
				t.Fatalf("END output = %q", out.Body)
			}
		} else {
			if out.Body != "***" {
				t.Fatalf("body line %q output = %q", line, out.Body)
			}
		}
	}

	// after END, normal line
	rec.Body = "2026-10-26 12:00:01 INFO after"
	out = redactLogRecord(rec)
	if out.Body != rec.Body {
		t.Fatalf("after END output = %q, want unchanged", out.Body)
	}

	if got := pemState.len(); got != 0 {
		t.Fatalf("pemState.len() = %d, want 0", got)
	}
}

func TestRedactPEMRealKeyOtherSourceInterleaved(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	keyLines := generatePEMKeyLines(t)
	probeA := "srcA"
	probeB := "srcB"
	attrsA := map[string]string{"log.file.path": "/var/log/a.log"}
	attrsB := map[string]string{"log.file.path": "/var/log/b.log"}

	outputsA := []string{}
	for i, line := range keyLines {
		recA := LogRecord{ProducerProbeName: probeA, Attributes: attrsA, Body: line}
		outA := redactLogRecord(recA)
		outputsA = append(outputsA, outA.Body)

		recB := LogRecord{ProducerProbeName: probeB, Attributes: attrsB, Body: fmt.Sprintf("B line %d", i)}
		outB := redactLogRecord(recB)
		if outB.Body != recB.Body {
			t.Fatalf("source B line %d masked incorrectly: %q", i, outB.Body)
		}
	}

	assertNoKeyLeak(t, outputsA, keyLines)
	if got := pemState.len(); got != 0 {
		t.Fatalf("pemState.len() = %d, want 0", got)
	}
}

func TestRedactPEMBeginWithoutEndHitsBound(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	var mu sync.Mutex
	type call struct {
		probe, source  string
		records, bytes int
	}
	var calls []call
	SetPEMOverflowHandler(func(probe, source string, records, bytes int) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call{probe, source, records, bytes})
	})
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	probe := "tail"
	attrs := map[string]string{"log.file.path": "/var/log/bound.log"}
	rec := LogRecord{ProducerProbeName: probe, Attributes: attrs}

	// BEGIN (opens state)
	rec.Body = "-----BEGIN RSA PRIVATE KEY-----"
	out := redactLogRecord(rec)
	if out.Body != "-----BEGIN RSA PRIVATE KEY-----***" {
		t.Fatalf("BEGIN output = %q", out.Body)
	}

	const totalRecords = 250
	for i := 0; i < totalRecords; i++ {
		rec.Body = "QUJDREVGRw=="
		out = redactLogRecord(rec)
		if i < 200 {
			if out.Body != "***" {
				t.Fatalf("record %d: output = %q, want ***", i, out.Body)
			}
		} else {
			if out.Body != rec.Body {
				t.Fatalf("record %d: output = %q, want unchanged", i, out.Body)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("overflow handler calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.probe != probe || c.source != "/var/log/bound.log" || c.records != 200 || c.bytes != 200*len("QUJDREVGRw==") {
		t.Fatalf("overflow call = %+v", c)
	}

	if got := pemState.len(); got != 0 {
		t.Fatalf("pemState.len() = %d, want 0", got)
	}
}

func TestRedactPEMByteBound(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	var mu sync.Mutex
	var calls [][4]interface{}
	SetPEMOverflowHandler(func(probe, source string, records, bytes int) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, [4]interface{}{probe, source, records, bytes})
	})
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	probe := "byte"
	attrs := map[string]string{"log.file.path": "/var/log/byte.log"}
	rec := LogRecord{ProducerProbeName: probe, Attributes: attrs}

	rec.Body = "-----BEGIN RSA PRIVATE KEY-----"
	redactLogRecord(rec)

	const blockSize = 4096
	line := strings.Repeat("A", blockSize)
	// 16 records: 16*4096 = 65536 >= 64<<10 (65536)
	for i := 0; i < 17; i++ {
		rec.Body = line
		out := redactLogRecord(rec)
		if i < 16 {
			if out.Body != "***" {
				t.Fatalf("record %d: output = %q, want ***", i, out.Body)
			}
		} else {
			if out.Body != line {
				t.Fatalf("record %d: output should be unchanged", i)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("overflow handler calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c[0] != probe || c[1] != "/var/log/byte.log" || c[2] != 16 || c[3] != 16*blockSize {
		t.Fatalf("overflow call = %v", c)
	}
	if got := pemState.len(); got != 0 {
		t.Fatalf("pemState.len() = %d, want 0", got)
	}
}

func TestRedactPEMRealKeySingleLine(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	keyLines := generatePEMKeyLines(t)
	fullPEM := strings.Join(keyLines, "\n")

	rec := LogRecord{ProducerProbeName: "single", Attributes: map[string]string{"log.file.path": "/single"}, Body: fullPEM}
	out := redactLogRecord(rec)
	expected := "-----BEGIN PRIVATE KEY-----***-----END PRIVATE KEY-----"
	if out.Body != expected {
		t.Fatalf("single line output = %q, want %q", out.Body, expected)
	}

	// A body‑looking line afterwards must be unchanged.
	rec.Body = "QUJDREVGRw=="
	out = redactLogRecord(rec)
	if out.Body != rec.Body {
		t.Fatalf("after single line, output = %q, want unchanged", out.Body)
	}
}

func TestRedactPEMRealKeyMultilineGrouping(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	keyLines := generatePEMKeyLines(t)
	probe := "group"
	attrs := map[string]string{"log.file.path": "/var/log/group.log"}

	// record1: prefix + BEGIN + first 10 body lines
	firstPart := "2026-10-26 ERROR dumping\n" + keyLines[0] + "\n" + strings.Join(keyLines[1:11], "\n")
	rec := LogRecord{ProducerProbeName: probe, Attributes: attrs, Body: firstPart}
	out1 := redactLogRecord(rec)
	if want := "2026-10-26 ERROR dumping\n-----BEGIN PRIVATE KEY-----***"; out1.Body != want {
		t.Fatalf("record1 output = %q, want %q", out1.Body, want)
	}
	for _, line := range keyLines[1:11] {
		if strings.Contains(out1.Body, line) {
			t.Fatalf("body line %q leaked in record1", line)
		}
	}

	// record2: remaining body lines + END + trailing text
	remaining := keyLines[11:]
	endIdx := len(remaining) - 1
	secondPart := strings.Join(remaining[:endIdx], "\n") + "\n" + remaining[endIdx] + "\ntrailing token=abc"
	rec.Body = secondPart
	out2 := redactLogRecord(rec)

	// Expected: masked END then maskString applied to tail.
	tail := "\ntrailing token=abc"
	rs := loadRedactionRuleset()
	maskedTail := maskString(rs, tail, probe)
	expected := "***" + remaining[endIdx] + maskedTail
	if out2.Body != expected {
		t.Fatalf("record2 output = %q, want %q", out2.Body, expected)
	}
	// Ensure no body line leaked.
	for _, line := range remaining[:endIdx] {
		if strings.Contains(out2.Body, line) {
			t.Fatalf("body line %q leaked in record2", line)
		}
	}

	if got := pemState.len(); got != 0 {
		t.Fatalf("pemState.len() = %d, want 0", got)
	}
}

func TestRedactPEMBatchOrdinaryLineAfterBegin(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	keyLines := generatePEMKeyLines(t)

	attrs := map[string]string{"log.file.path": "/var/log/batch.log"}

	records := []*logspb.LogRecord{}
	addRec := func(body string) {
		records = append(records, &logspb.LogRecord{
			Body: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: body},
			},
			Attributes: []*commonpb.KeyValue{
				{
					Key: "log.file.path",
					Value: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: attrs["log.file.path"]},
					},
				},
			},
		})
	}

	// BEGIN
	addRec(keyLines[0])
	// ordinary line
	addRec("2026-10-26 12:00:00 INFO worker started")
	// body lines and END
	for _, line := range keyLines[1:] {
		addRec(line)
	}

	batch := &logspb.ResourceLogs{
		ScopeLogs: []*logspb.ScopeLogs{
			{
				LogRecords: records,
			},
		},
	}
	RedactLogBatches([]*logspb.ResourceLogs{batch}, "otlp")

	outputs := []string{}
	for _, lr := range records {
		body := lr.GetBody().GetStringValue()
		outputs = append(outputs, body)
	}

	// First record (BEGIN) will be masked by maskString: "-----BEGIN PRIVATE KEY-----***"
	if outputs[0] != "-----BEGIN PRIVATE KEY-----***" {
		t.Fatalf("batch BEGIN output = %q", outputs[0])
	}
	// Second (ordinary) must be "***"
	if outputs[1] != "***" {
		t.Fatalf("batch ordinary output = %q", outputs[1])
	}
	// Body lines and END must be "***" and masked END.
	for i, line := range keyLines[1:] {
		idx := i + 2
		if line == keyLines[len(keyLines)-1] {
			if outputs[idx] != "***"+line {
				t.Fatalf("batch END output = %q", outputs[idx])
			}
		} else {
			if outputs[idx] != "***" {
				t.Fatalf("batch body line output = %q", outputs[idx])
			}
		}
	}

	assertNoKeyLeak(t, outputs, keyLines)
	if got := batchPemState.len(); got != 0 {
		t.Fatalf("batchPemState.len() = %d, want 0", got)
	}
}

func TestRedactPEMOverflowHandlerOutsideLock(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	done := make(chan struct{})
	SetPEMOverflowHandler(func(probe, source string, records, bytes int) {
		// This call, if executed under the state lock, would deadlock because
		// it would attempt to redact a record while the lock is held.
		rec := LogRecord{
			ProducerProbeName: "handler",
			Attributes:        map[string]string{"log.file.path": "/tmp/handler.log"},
			Body:              "should not deadlock",
		}
		_ = redactLogRecord(rec)
		close(done)
	})
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	rec := LogRecord{
		ProducerProbeName: "overflow",
		Attributes:        map[string]string{"log.file.path": "/var/log/overflow.log"},
		Body:              "-----BEGIN RSA PRIVATE KEY-----",
	}
	redactLogRecord(rec)

	// Send enough records to exhaust the budget.
	for i := 0; i < 220; i++ {
		rec.Body = "QUJDREVGRw=="
		redactLogRecord(rec)
	}

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("overflow handler did not run or deadlocked")
	}
}

func TestRedactPEMStateKeepsMaskingOrdinaryLine(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	t.Cleanup(func() { SetPEMOverflowHandler(nil) })

	probe := "state"
	attrs := map[string]string{"log.file.path": "/var/log/state.log"}
	rec := LogRecord{ProducerProbeName: probe, Attributes: attrs}

	// BEGIN
	rec.Body = "-----BEGIN RSA PRIVATE KEY-----"
	out := redactLogRecord(rec)
	if out.Body != "-----BEGIN RSA PRIVATE KEY-----***" {
		t.Fatalf("BEGIN output = %q", out.Body)
	}

	// ordinary line
	rec.Body = "2026-10-26 INFO started"
	out = redactLogRecord(rec)
	if out.Body != "***" {
		t.Fatalf("ordinary line output = %q, want ***", out.Body)
	}

	// body‑looking line
	rec.Body = "AAAA"
	out = redactLogRecord(rec)
	if out.Body != "***" {
		t.Fatalf("body-looking line output = %q, want ***", out.Body)
	}

	if got := privateKeyRedactionCount(t, probe); got != 3 {
		t.Fatalf("redaction count = %d, want 3", got)
	}
}

func TestResumePEMBlockMasksRestOfKey(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	keyLines := generatePEMKeyLines(t)
	attrs := map[string]string{"log.file.path": "/var/log/resume.log"}
	ResumePEMBlock("tail", map[string]string{"log.file.path": "/var/log/resume.log"})

	var outputs []string
	for _, line := range keyLines[10:] {
		outputs = append(outputs, redactLogRecord(LogRecord{ProducerProbeName: "tail", Attributes: attrs, Body: line}).Body)
	}
	assertNoKeyLeak(t, outputs, keyLines)
	if got := outputs[len(outputs)-1]; got != "***-----END PRIVATE KEY-----" {
		t.Fatalf("END output = %q", got)
	}
	other := redactLogRecord(LogRecord{ProducerProbeName: "tail", Attributes: map[string]string{"log.file.path": "/var/log/other.log"}, Body: keyLines[3]}).Body
	if other != keyLines[3] {
		t.Fatalf("other source output = %q, want unchanged", other)
	}
	if pemState.len() != 0 {
		t.Fatalf("pem state has %d entries", pemState.len())
	}
}

func TestResumePEMBlockNoopWhenRuleOff(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(false, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	ResumePEMBlock("tail", map[string]string{"log.file.path": "/var/log/off.log"})
	if pemState.len() != 0 {
		t.Fatalf("pem state has %d entries with the rule off", pemState.len())
	}
}
