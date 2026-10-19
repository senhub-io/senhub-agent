package agentstate

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupRedactionTest(t *testing.T) {
	t.Helper()
	resetLogRedactionForTest()
	t.Cleanup(resetLogRedactionForTest)
}

func setupLogChannelTest(t *testing.T) {
	t.Helper()
	resetLogChannelForTest()
	t.Cleanup(resetLogChannelForTest)
}

func TestRedactPresetPositive(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "query passhash",
			in:   "GET /api/x?passhash=123&x=1",
			want: "GET /api/x?passhash=***&x=1",
		},
		{
			name: "yaml password",
			in:   "password: hunter2",
			want: "password: ***",
		},
		{
			name: "json secret",
			in:   `{"secret":"abc"}`,
			want: `{"secret":"***"}`,
		},
		{
			name: "api key quoted",
			in:   `api_key='k'`,
			want: `api_key='***'`,
		},
		{
			name: "token semicolon",
			in:   "Token=ABC;",
			want: "Token=***;",
		},
		{
			name: "traefik access log",
			in:   `10.0.0.5 - - [19/Oct/2026:10:00:00 +0000] "GET /api/table.json?content=sensors&username=prtgadmin&passhash=1234567890 HTTP/1.1" 200 512 "-" "curl/8"`,
			want: `10.0.0.5 - - [19/Oct/2026:10:00:00 +0000] "GET /api/table.json?content=sensors&username=prtgadmin&passhash=*** HTTP/1.1" 200 512 "-" "curl/8"`,
		},
		{
			name: "authorization basic",
			in:   "Authorization: Basic dXNlcjpwYXNz",
			want: "Authorization: Basic ***",
		},
		{
			name: "authorization bearer lowercase header",
			in:   "authorization: Bearer abc.def",
			want: "authorization: Bearer ***",
		},
		{
			name: "json body with multiple fields",
			in:   `{"user":"a","password":"p w","token":"t1"}`,
			want: `{"user":"a","password":"*** w","token":"***"}`,
		},
		{
			name: "authorization digest rest of line",
			in:   `Authorization: Digest username="bob", realm="x"`,
			want: `Authorization: Digest ***`,
		},
		{
			name: "authorization bare",
			in:   "Authorization: abc123",
			want: "Authorization: ***",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := redactLogRecord(LogRecord{Body: tc.in, ProducerProbeName: "probe"})
			if rec.Body != tc.want {
				t.Fatalf("Body = %q, want %q", rec.Body, tc.want)
			}
		})
	}
}

func TestRedactPresetNegative(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	cases := []struct {
		name string
		in   string
	}{
		{name: "tokens plural", in: "tokens=5"},
		{name: "mytoken", in: "mytoken=abc"},
		{name: "empty passhash", in: "passhash="},
		{name: "password word alone", in: "password"},
		{name: "no keyword", in: "GET /api/status?x=1 200 512"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := redactLogRecord(LogRecord{Body: tc.in, ProducerProbeName: "probe"})
			if rec.Body != tc.in {
				t.Fatalf("Body = %q, want unchanged %q", rec.Body, tc.in)
			}
		})
	}
}

func TestRedactCustomRuleAfterPreset(t *testing.T) {
	setupRedactionTest(t)
	errs := ConfigureLogRedaction(true, []LogRedactRule{
		{Name: "card", Pattern: `(\b\d{4})\d{8}(\d{4}\b)`, Replacement: `$1********$2`},
	})
	if len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	rec := redactLogRecord(LogRecord{Body: "card 1234567890123456 password: x", ProducerProbeName: "probe"})
	want := "card 1234********3456 password: ***"
	if rec.Body != want {
		t.Fatalf("Body = %q, want %q", rec.Body, want)
	}

	counts := LogRedactedCounts()
	var cardCount uint64
	for _, c := range counts {
		if c.Probe == "probe" && c.Rule == "card" {
			cardCount = c.Count
		}
	}
	if cardCount != 1 {
		t.Fatalf("card count = %d, want 1", cardCount)
	}
}

func TestRedactInvalidRules(t *testing.T) {
	setupRedactionTest(t)
	rules := []LogRedactRule{
		{Name: "Bad Name", Pattern: `x`, Replacement: `bad`},
		{Name: "ok", Pattern: `[`, Replacement: `no`},
		{Name: "card", Pattern: `(\b\d{4})\d{8}(\d{4}\b)`, Replacement: `$1********$2`},
	}

	errs := ValidateLogRedactRules(rules)
	if len(errs) != 2 {
		t.Fatalf("ValidateLogRedactRules returned %d errors, want 2: %v", len(errs), errs)
	}
	for _, err := range errs {
		if strings.Contains(err.Error(), "bad") {
			t.Fatalf("error contains replacement: %v", err)
		}
	}

	errs = ConfigureLogRedaction(true, rules)
	if len(errs) != 2 {
		t.Fatalf("ConfigureLogRedaction returned %d errors, want 2: %v", len(errs), errs)
	}

	var sawBadName, sawOK bool
	for _, err := range errs {
		if strings.Contains(err.Error(), "Bad Name") {
			sawBadName = true
		}
		if strings.Contains(err.Error(), `"ok"`) && strings.Contains(err.Error(), "pattern") {
			sawOK = true
		}
	}
	if !sawBadName || !sawOK {
		t.Fatalf("errors do not name bad rules: %v", errs)
	}

	rec := redactLogRecord(LogRecord{Body: "card 1234567890123456 password: x", ProducerProbeName: "probe"})
	want := "card 1234********3456 password: ***"
	if rec.Body != want {
		t.Fatalf("Body = %q, want %q", rec.Body, want)
	}
}

func TestRedactPresetFalse(t *testing.T) {
	setupRedactionTest(t)
	errs := ConfigureLogRedaction(false, []LogRedactRule{
		{Name: "card", Pattern: `(\b\d{4})\d{8}(\d{4}\b)`, Replacement: `$1********$2`},
	})
	if len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	rec := redactLogRecord(LogRecord{Body: "passhash=123 card 1234567890123456", ProducerProbeName: "probe"})
	want := "passhash=123 card 1234********3456"
	if rec.Body != want {
		t.Fatalf("Body = %q, want %q", rec.Body, want)
	}
}

func TestRedactAttributesAndFieldsCopyOnWrite(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	attrs := map[string]string{"url.query": "a=1&password=hunter2", "no": "x"}
	fields := map[string]any{
		"top": "password: aa",
		"nested": map[string]any{
			"note": "token=t1",
			"arr":  []any{"password: p", "token=abc"},
		},
	}

	rec := redactLogRecord(LogRecord{
		Attributes:        attrs,
		Fields:            fields,
		ProducerProbeName: "probe",
	})

	if !reflect.DeepEqual(rec.Attributes, map[string]string{"url.query": "a=1&password=***", "no": "x"}) {
		t.Fatalf("Attributes = %#v", rec.Attributes)
	}
	if attrs["url.query"] != "a=1&password=hunter2" {
		t.Fatalf("original Attributes mutated: %#v", attrs)
	}

	wantFields := map[string]any{
		"top": "password: ***",
		"nested": map[string]any{
			"note": "token=***",
			"arr":  []any{"password: ***", "token=***"},
		},
	}
	if !reflect.DeepEqual(rec.Fields, wantFields) {
		t.Fatalf("Fields = %#v, want %#v", rec.Fields, wantFields)
	}

	origNested := fields["nested"].(map[string]any)
	if origNested["note"] != "token=t1" {
		t.Fatalf("original nested map mutated: %#v", origNested)
	}
	origArr := origNested["arr"].([]any)
	if !reflect.DeepEqual(origArr, []any{"password: p", "token=abc"}) {
		t.Fatalf("original slice mutated: %#v", origArr)
	}
}

func TestRedactedCountsSortedAndNotResetByConfigure(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	_ = redactLogRecord(LogRecord{ProducerProbeName: "probe-a", Body: "password: a token=abc"})
	_ = redactLogRecord(LogRecord{ProducerProbeName: "probe-b", Body: "Authorization: Basic abc"})
	_ = redactLogRecord(LogRecord{ProducerProbeName: "probe-a", Body: "password: b"})

	if errs := ConfigureLogRedaction(false, []LogRedactRule{{Name: "x", Pattern: "x", Replacement: "y"}}); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	counts := LogRedactedCounts()
	want := []LogRedactedCount{
		{Probe: "probe-a", Rule: "credential_param", Count: 3},
		{Probe: "probe-b", Rule: "authorization_header", Count: 1},
	}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("counts = %#v, want %#v", counts, want)
	}
}

func TestRedactReload(t *testing.T) {
	setupRedactionTest(t)
	errs := ConfigureLogRedaction(false, []LogRedactRule{
		{Name: "a", Pattern: "alpha", Replacement: "ALPHA"},
	})
	if len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	rec := redactLogRecord(LogRecord{Body: "alpha beta", ProducerProbeName: "probe"})
	if rec.Body != "ALPHA beta" {
		t.Fatalf("first Body = %q, want %q", rec.Body, "ALPHA beta")
	}

	errs = ConfigureLogRedaction(false, []LogRedactRule{
		{Name: "b", Pattern: "beta", Replacement: "BETA"},
	})
	if len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	rec = redactLogRecord(LogRecord{Body: "alpha beta", ProducerProbeName: "probe"})
	if rec.Body != "alpha BETA" {
		t.Fatalf("second Body = %q, want %q", rec.Body, "alpha BETA")
	}
}

func TestRedactEndToEndPublishLog(t *testing.T) {
	setupRedactionTest(t)
	setupLogChannelTest(t)

	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	ch := SubscribeLogs(1)
	defer UnsubscribeLogs(ch)

	PublishLog(LogRecord{Body: "GET /x?passhash=123", ProducerProbeName: "probe"})

	select {
	case rec := <-ch:
		if rec.Body != "GET /x?passhash=***" {
			t.Fatalf("Body = %q, want %q", rec.Body, "GET /x?passhash=***")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for masked log record")
	}
}

func TestRedactionConcurrency(t *testing.T) {
	setupRedactionTest(t)
	setupLogChannelTest(t)

	ch := SubscribeLogs(1)
	defer UnsubscribeLogs(ch)

	t.Run("configure and publish", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 50; i++ {
				preset := i%2 == 0
				_ = ConfigureLogRedaction(preset, []LogRedactRule{
					{Name: "extra", Pattern: "extra", Replacement: "EXTRA"},
				})
			}
		}()

		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 200; i++ {
					PublishLog(LogRecord{Body: "password: hunter2 extra", ProducerProbeName: "probe"})
				}
			}()
		}
		wg.Wait()
		<-done
	})

	t.Run("constant counters", func(t *testing.T) {
		setupRedactionTest(t)
		if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
			t.Fatalf("ConfigureLogRedaction errors: %v", errs)
		}

		const goroutines = 8
		const per = 200

		var wg sync.WaitGroup
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < per; i++ {
					PublishLog(LogRecord{Body: "passhash=123", ProducerProbeName: "probe"})
				}
			}()
		}
		wg.Wait()

		counts := LogRedactedCounts()
		var total uint64
		for _, c := range counts {
			if c.Probe == "probe" && c.Rule == "credential_param" {
				total = c.Count
			}
		}
		if total != goroutines*per {
			t.Fatalf("credential_param count = %d, want %d", total, goroutines*per)
		}
	})
}

func BenchmarkRedactNoSecret(b *testing.B) {
	_ = ConfigureLogRedaction(true, nil)
	rec := LogRecord{
		Body:              `10.0.0.5 - - [19/Oct/2026:10:00:00 +0000] "GET /api/status?x=1 HTTP/1.1" 200 512 "-" "curl/8"`,
		ProducerProbeName: "bench",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = redactLogRecord(rec)
	}
}

func BenchmarkRedactPasshash(b *testing.B) {
	_ = ConfigureLogRedaction(true, nil)
	rec := LogRecord{
		Body:              `10.0.0.5 - - [19/Oct/2026:10:00:00 +0000] "GET /api/status?x=1&passhash=123 HTTP/1.1" 200 512 "-" "curl/8"`,
		ProducerProbeName: "bench",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = redactLogRecord(rec)
	}
}
