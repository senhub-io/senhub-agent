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

// An attribute named after a credential holds the secret itself, with no
// "key=" form for the patterns to find.
func TestRedactAttributeByCredentialKey(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	rec := redactLogRecord(LogRecord{
		Attributes: map[string]string{
			"password":                          "hunter2",
			"db.Password":                       "s3cret",
			"http.request.header.authorization": "Bearer abc.def",
			"user.name":                         "alice",
			"tokens":                            "5",
		},
		ProducerProbeName: "p",
	})
	want := map[string]string{
		"password":                          "***",
		"db.Password":                       "***",
		"http.request.header.authorization": "Bearer ***",
		"user.name":                         "alice",
		"tokens":                            "5",
	}
	if !reflect.DeepEqual(rec.Attributes, want) {
		t.Fatalf("Attributes = %#v, want %#v", rec.Attributes, want)
	}

	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(false, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}
	off := redactLogRecord(LogRecord{Attributes: map[string]string{"password": "hunter2"}, ProducerProbeName: "p"})
	if off.Attributes["password"] != "hunter2" {
		t.Fatalf("preset off must leave keyed attributes alone, got %q", off.Attributes["password"])
	}
}

// PRTG writes its licence key and system id in clear in Core.log and
// CoreActivationLog.log on every core start. A probe that ships those
// files must never carry them, whatever its own configuration.
func TestRedactPresetMasksThePRTGLicence(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	const key = "ABC123-DEF456-GHI789-JKL012-MNO345-PQR678-STU901-VWX234-YZA567-BCD890"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "licence line",
			in:   `2026-10-20 09:30:01.000001 INFO  TId 1 Core> Paessler PRTG Enterprise licensed for "Example" (` + key + `) Edt=7`,
			want: `2026-10-20 09:30:01.000001 INFO  TId 1 Core> Paessler PRTG Enterprise licensed for "Example" (***) Edt=7`,
		},
		{
			name: "system id line",
			in:   "2026-10-20 09:30:01.000002 INFO  TId 1 Core> SystemID: SYSTEMID-ABCDEF12-34567890-ABCDEF12-34567890-ABCDEF12",
			want: "2026-10-20 09:30:01.000002 INFO  TId 1 Core> SystemID: SYSTEMID-***",
		},
		{
			name: "system id with more groups than today",
			in:   "Core> SystemID: SYSTEMID-ABCDEF12-34567890-ABCDEF12-34567890-ABCDEF12-1ABCDEFG done",
			want: "Core> SystemID: SYSTEMID-*** done",
		},
		{
			name: "both inside a folded multiline record",
			in:   "Core> licensed for \"Example\" (" + key + ")\nCore> SystemID: SYSTEMID-ABCDEF12-34567890-ABCDEF12-34567890-ABCDEF12",
			want: "Core> licensed for \"Example\" (***)\nCore> SystemID: SYSTEMID-***",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := redactLogRecord(LogRecord{Body: tc.in, ProducerProbeName: "prtg-logs"})
			if rec.Body != tc.want {
				t.Fatalf("Body = %q, want %q", rec.Body, tc.want)
			}
		})
	}

	counts := map[string]uint64{}
	for _, c := range LogRedactedCounts() {
		if c.Probe == "prtg-logs" {
			counts[c.Rule] = c.Count
		}
	}
	if counts["prtg_license_key"] != 2 || counts["prtg_system_id"] != 3 {
		t.Fatalf("counters = %v, want 2 masked keys and 3 masked system ids", counts)
	}

	for _, keep := range []string{
		"Core> sensor 2413 ABC123-DEF456 scanned",
		"guid 0f4c2a51-9b7e-4d3a-8c61-2e5f7a9b0d13",
		"ABC123-DEF456-GHI789-JKL012-MNO345-PQR678-STU901-VWX234-YZA567",
		"abc123-def456-ghi789-jkl012-mno345-pqr678-stu901-vwx234-yza567-bcd890",
	} {
		if rec := redactLogRecord(LogRecord{Body: keep, ProducerProbeName: "prtg-logs"}); rec.Body != keep {
			t.Errorf("Body = %q, want unchanged %q", rec.Body, keep)
		}
	}
}

func TestRedactPresetRuleNamesAreReserved(t *testing.T) {
	for _, name := range []string{"prtg_license_key", "prtg_system_id", "private_key", "jwt", "aws_access_key_id", "aws_secret_access_key"} {
		errs := ValidateLogRedactRules([]LogRedactRule{{Name: name, Pattern: "x", Replacement: "y"}})
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "reserved") {
			t.Errorf("custom rule %q: errors %v, want it reserved", name, errs)
		}
	}
}

func TestRedactPresetMasksKeysAndTokens(t *testing.T) {
	setupRedactionTest(t)
	if errs := ConfigureLogRedaction(true, nil); len(errs) != 0 {
		t.Fatalf("ConfigureLogRedaction errors: %v", errs)
	}

	const jwt = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "pem private key",
			in:   "loaded key\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA1c3\nabcDEF==\n-----END RSA PRIVATE KEY-----\ndone",
			want: "loaded key\n-----BEGIN RSA PRIVATE KEY-----***-----END RSA PRIVATE KEY-----\ndone",
		},
		{
			name: "pem private key cut short",
			in:   "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0",
			want: "-----BEGIN PRIVATE KEY-----***",
		},
		{
			name: "jwt in a log line",
			in:   "session " + jwt + " issued",
			want: "session *** issued",
		},
		{
			name: "aws access key id",
			in:   "using key AKIAIOSFODNN7EXAMPLE for s3",
			want: "using key *** for s3",
		},
		{
			name: "aws temporary key id",
			in:   "creds ASIAY34FZKBOKMUTVV7A",
			want: "creds ***",
		},
		{
			name: "aws secret access key",
			in:   "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			want: "aws_secret_access_key = ***",
		},
		{
			name: "aws secret access key in json",
			in:   `{"aws_secret_access_key":"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}`,
			want: `{"aws_secret_access_key":"***"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := redactLogRecord(LogRecord{Body: tc.in, ProducerProbeName: "app-logs"})
			if rec.Body != tc.want {
				t.Fatalf("Body = %q, want %q", rec.Body, tc.want)
			}
		})
	}

	for _, keep := range []string{
		"-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqh\n-----END PUBLIC KEY-----",
		"-----BEGIN CERTIFICATE-----\nMIIDdzCCAl+g\n-----END CERTIFICATE-----",
		"payload eyJhbGciOiJIUzI1NiJ9 seen",
		"a.b.c eyJ.eyJ.x",
		"key AKIA1234 too short",
		"akiaiosfodnn7example lowercase",
		"secret_access_key = short",
		"aws_secret_access_key = notfortycharacters",
	} {
		if rec := redactLogRecord(LogRecord{Body: keep, ProducerProbeName: "app-logs"}); rec.Body != keep {
			t.Errorf("Body = %q, want unchanged %q", rec.Body, keep)
		}
	}
}
