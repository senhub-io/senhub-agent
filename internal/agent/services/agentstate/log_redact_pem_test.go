package agentstate

import (
	"reflect"
	"strconv"
	"sync"
	"testing"
)

func privateKeyRedactionCount(t *testing.T, probe string) uint64 {
	t.Helper()
	var count uint64
	for _, c := range LogRedactedCounts() {
		if c.Probe == probe && c.Rule == "private_key" {
			count = c.Count
		}
	}
	return count
}

func TestRedactPEMSplitOneRecordPerLine(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	path := "/var/log/a.log"
	rec := func(body string) LogRecord {
		return LogRecord{
			ProducerProbeName: "tail",
			Attributes:        map[string]string{"log.file.path": path},
			Body:              body,
		}
	}

	bodies := []string{
		"-----BEGIN RSA PRIVATE KEY-----",
		"MIIEpAIBAAKCAQEA1c3",
		"abcDEF0123+/",
		"xyz==",
		"-----END RSA PRIVATE KEY-----",
	}
	want := []string{
		"-----BEGIN RSA PRIVATE KEY-----***",
		"***",
		"***",
		"***",
		"***-----END RSA PRIVATE KEY-----",
	}

	got := make([]string, 0, len(bodies))
	for _, body := range bodies {
		got = append(got, redactLogRecord(rec(body)).Body)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bodies = %q, want %q", got, want)
	}
	if cnt := privateKeyRedactionCount(t, "tail"); cnt != 5 {
		t.Fatalf("private_key count = %d, want 5", cnt)
	}
}

func TestRedactPEMInterleavedSource(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	pathA := "/var/log/a.log"
	pathB := "/var/log/b.log"
	recA := func(body string) LogRecord {
		return LogRecord{
			ProducerProbeName: "tail",
			Attributes:        map[string]string{"log.file.path": pathA},
			Body:              body,
		}
	}
	recB := func(body string) LogRecord {
		return LogRecord{
			ProducerProbeName: "tail",
			Attributes:        map[string]string{"log.file.path": pathB},
			Body:              body,
		}
	}

	got := []string{
		redactLogRecord(recA("-----BEGIN RSA PRIVATE KEY-----")).Body,
		redactLogRecord(recA("MIIEpAIBAAKCAQEA1c3")).Body,
		redactLogRecord(recB("hello world")).Body,
		redactLogRecord(recA("abcDEF0123+/")).Body,
		redactLogRecord(recA("xyz==")).Body,
		redactLogRecord(recA("-----END RSA PRIVATE KEY-----")).Body,
	}
	want := []string{
		"-----BEGIN RSA PRIVATE KEY-----***",
		"***",
		"hello world",
		"***",
		"***",
		"***-----END RSA PRIVATE KEY-----",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bodies = %q, want %q", got, want)
	}
	if cnt := privateKeyRedactionCount(t, "tail"); cnt != 5 {
		t.Fatalf("private_key count = %d, want 5", cnt)
	}
}

func TestRedactPEMDisabledLeavesLinesUntouched(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(false, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	path := "/var/log/d.log"
	rec := func(body string) LogRecord {
		return LogRecord{
			ProducerProbeName: "tail",
			Attributes:        map[string]string{"log.file.path": path},
			Body:              body,
		}
	}

	bodies := []string{
		"-----BEGIN RSA PRIVATE KEY-----",
		"MIIEpAIBAAKCAQEA1c3",
		"AAAA",
	}
	for _, body := range bodies {
		if got := redactLogRecord(rec(body)).Body; got != body {
			t.Fatalf("body = %q, want unchanged %q", got, body)
		}
	}
	if cnt := privateKeyRedactionCount(t, "tail"); cnt != 0 {
		t.Fatalf("private_key count = %d, want 0", cnt)
	}
}

func TestRedactPEMHardCap(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	path := "/var/log/cap.log"
	rec := func(body string) LogRecord {
		return LogRecord{
			ProducerProbeName: "tail",
			Attributes:        map[string]string{"log.file.path": path},
			Body:              body,
		}
	}

	begin := redactLogRecord(rec("-----BEGIN RSA PRIVATE KEY-----")).Body
	if begin != "-----BEGIN RSA PRIVATE KEY-----***" {
		t.Fatalf("begin = %q, want %q", begin, "-----BEGIN RSA PRIVATE KEY-----***")
	}

	const total = 205
	for i := 1; i <= total; i++ {
		got := redactLogRecord(rec("QUJDREVGRw==")).Body
		want := "QUJDREVGRw=="
		if i <= 200 {
			want = "***"
		}
		if got != want {
			t.Fatalf("line %d = %q, want %q", i, got, want)
		}
		if i == 200 && pemState.len() != 0 {
			t.Fatalf("state not cleared after 200th masked body line")
		}
	}

	if pemState.len() != 0 {
		t.Fatalf("pem state has %d entries after cap", pemState.len())
	}
	if cnt := privateKeyRedactionCount(t, "tail"); cnt != 201 {
		t.Fatalf("private_key count = %d, want 201", cnt)
	}
}

func TestRedactPEMConcurrentSources(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	const goroutines = 8
	const bodyLines = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()

			path := "/var/log/concurrent-" + strconv.Itoa(g) + ".log"
			rec := func(body string) LogRecord {
				return LogRecord{
					ProducerProbeName: "tail",
					Attributes:        map[string]string{"log.file.path": path},
					Body:              body,
				}
			}

			if got := redactLogRecord(rec("-----BEGIN RSA PRIVATE KEY-----")).Body; got != "-----BEGIN RSA PRIVATE KEY-----***" {
				t.Errorf("goroutine %d begin = %q", g, got)
				return
			}
			for i := 0; i < bodyLines; i++ {
				if got := redactLogRecord(rec("QUJDREVGRw==")).Body; got != "***" {
					t.Errorf("goroutine %d body %d = %q", g, i, got)
					return
				}
			}
			if got := redactLogRecord(rec("-----END RSA PRIVATE KEY-----")).Body; got != "***-----END RSA PRIVATE KEY-----" {
				t.Errorf("goroutine %d end = %q", g, got)
				return
			}
		}(g)
	}
	wg.Wait()

	if pemState.len() != 0 {
		t.Fatalf("pem state has %d entries after concurrent flush", pemState.len())
	}
	if cnt := privateKeyRedactionCount(t, "tail"); cnt != goroutines*(bodyLines+2) {
		t.Fatalf("private_key count = %d, want %d", cnt, goroutines*(bodyLines+2))
	}
}

func TestRedactPEMBeginAndEndSameRecordDoesNotEnterState(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	path := "/var/log/same.log"
	rec := func(body string) LogRecord {
		return LogRecord{
			ProducerProbeName: "tail",
			Attributes:        map[string]string{"log.file.path": path},
			Body:              body,
		}
	}

	in := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA1c3\n-----END RSA PRIVATE KEY-----"
	want := "-----BEGIN RSA PRIVATE KEY-----***-----END RSA PRIVATE KEY-----"
	if got := redactLogRecord(rec(in)).Body; got != want {
		t.Fatalf("same-record PEM body = %q, want %q", got, want)
	}

	const base64Line = "QUJDREVGRw=="
	if got := redactLogRecord(rec(base64Line)).Body; got != base64Line {
		t.Fatalf("following base64 line = %q, want %q", got, base64Line)
	}
	if pemState.len() != 0 {
		t.Fatalf("pem state has %d entries, want 0", pemState.len())
	}
	if cnt := privateKeyRedactionCount(t, "tail"); cnt != 1 {
		t.Fatalf("private_key count = %d, want 1", cnt)
	}
}
