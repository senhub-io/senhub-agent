package jenkins

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mustBuildJenkinsProbe(t *testing.T, raw map[string]interface{}) *jenkinsProbe {
	t.Helper()
	probe, err := NewJenkinsProbe(raw, testBaseLogger())
	if err != nil {
		t.Fatalf("NewJenkinsProbe: %v", err)
	}
	p := probe.(*jenkinsProbe)
	p.SetName("extra-ci")
	return p
}

func identityGetJSONForServer(srv *httptest.Server) func(string, interface{}) error {
	return func(path string, out interface{}) error {
		p := &jenkinsProbe{
			cfg:    jenkinsConfig{Endpoint: srv.URL},
			client: srv.Client(),
		}
		return p.getJSON(context.Background(), path, out)
	}
}

func TestHTTPStatusError_Error(t *testing.T) {
	err := &httpStatusError{code: http.StatusUnauthorized, msg: "jenkins /api/json returned HTTP 401: unauthorized"}
	if got, want := err.Error(), "jenkins /api/json returned HTTP 401: unauthorized"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestNewJenkinsProbe_BasicAuthFromConfig(t *testing.T) {
	const username = "alice"
	const apiToken = "tok-123"
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobs":[]}`))
	}))
	defer srv.Close()

	p := mustBuildJenkinsProbe(t, map[string]interface{}{
		"endpoint":  srv.URL,
		"username":  username,
		"api_token": apiToken,
	})
	var resp jobsResponse
	if err := p.getJSON(context.Background(), "/api/json", &resp); err != nil {
		t.Fatalf("getJSON: %v", err)
	}

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+apiToken))
	if gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestNewJenkinsProbe_UsesIntervalAndTimeoutConfig(t *testing.T) {
	p := mustBuildJenkinsProbe(t, map[string]interface{}{
		"endpoint": "http://127.0.0.1:8080",
		"interval": 75,
		"timeout":  9,
	})

	if got, want := p.GetInterval(), 75*time.Second; got != want {
		t.Errorf("GetInterval() = %v, want %v", got, want)
	}
	if got, want := p.client.Timeout, 9*time.Second; got != want {
		t.Errorf("client.Timeout = %v, want %v", got, want)
	}
}

func TestNewJenkinsProbe_DefaultIntervalWhenUnset(t *testing.T) {
	p := mustBuildJenkinsProbe(t, map[string]interface{}{"endpoint": "http://127.0.0.1:8080"})
	if got, want := p.GetInterval(), 60*time.Second; got != want {
		t.Errorf("GetInterval() = %v, want default %v", got, want)
	}
}

func TestNewJenkinsProbe_InvalidEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{"malformed URL", "://bad", true},
		{"no scheme host", "jenkins.example.com", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewJenkinsProbe(map[string]interface{}{"endpoint": tc.endpoint}, testBaseLogger())
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewJenkinsProbe error = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}

func TestJenkinsProbe_GetTargetStrategies(t *testing.T) {
	p := mustBuildJenkinsProbe(t, map[string]interface{}{"endpoint": "http://127.0.0.1:8080"})
	got := p.GetTargetStrategies()
	want := []string{"senhub", "prtg", "http", "otlp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetTargetStrategies() = %v, want %v", got, want)
	}
}

func TestJenkinsProbe_ShouldStartAlways(t *testing.T) {
	p := mustBuildJenkinsProbe(t, map[string]interface{}{"endpoint": "http://127.0.0.1:8080"})
	if !p.ShouldStart() {
		t.Fatal("ShouldStart() = false, want true")
	}
}

func TestJenkinsProbe_OnStartOnShutdownNoError(t *testing.T) {
	p := mustBuildJenkinsProbe(t, map[string]interface{}{"endpoint": "http://127.0.0.1:8080"})

	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if err := p.OnShutdown(context.Background()); err != nil {
		t.Fatalf("OnShutdown: %v", err)
	}
}

func TestNormalizeJobStatus_JenkinsResultAndColorCodes(t *testing.T) {
	cases := []struct {
		name   string
		result string
		color  string
		want   string
	}{
		{"success result", "SUCCESS", "", "success"},
		{"failure result", "FAILURE", "", "failure"},
		{"unstable result", "UNSTABLE", "", "unstable"},
		{"aborted result", "ABORTED", "", "aborted"},
		{"result overrides colour", "SUCCESS", "red", "success"},
		{"blue", "", "blue", "success"},
		{"blue_anime", "", "blue_anime", "success"},
		{"green", "", "green", "success"},
		{"green_anime", "", "green_anime", "success"},
		{"red", "", "red", "failure"},
		{"red_anime", "", "red_anime", "failure"},
		{"yellow", "", "yellow", "unstable"},
		{"yellow_anime", "", "yellow_anime", "unstable"},
		{"aborted", "", "aborted", "aborted"},
		{"aborted_anime", "", "aborted_anime", "aborted"},
		{"notbuilt", "", "notbuilt", ""},
		{"notbuilt_anime", "", "notbuilt_anime", ""},
		{"disabled", "", "disabled", ""},
		{"grey", "", "grey", ""},
		{"empty fallback", "", "", ""},
		{"unknown", "", "unknown", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeJobStatus(tc.result, tc.color); got != tc.want {
				t.Errorf("normalizeJobStatus(%q, %q) = %q, want %q", tc.result, tc.color, got, tc.want)
			}
		})
	}
}

func TestFetchInstanceIdentity_PublicKeyFingerprint(t *testing.T) {
	const publicKeyBase64 = "YWJj" // base64 of "abc"; SHA-256 first 10 bytes = ba7816bf8f01cfea4141

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance-identity/api/json" {
			t.Errorf("path = %q, want /instance-identity/api/json", r.URL.Path)
		}
		fmt.Fprintf(w, `{"publicKey":%q}`, publicKeyBase64)
	}))
	defer srv.Close()

	id, err := fetchInstanceIdentity(identityGetJSONForServer(srv))
	if err != nil {
		t.Fatalf("fetchInstanceIdentity: %v", err)
	}
	const want = "jenkins:ba7816bf8f01cfea4141"
	if id != want {
		t.Errorf("fetchInstanceIdentity() = %q, want %q", id, want)
	}
}

func TestFetchInstanceIdentity_HTTPStatusErrors(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantCode int
		wantMsg  string
	}{
		{"forbidden", http.StatusForbidden, http.StatusForbidden, "HTTP 403"},
		{"server error", http.StatusInternalServerError, http.StatusInternalServerError, "HTTP 500"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			_, err := fetchInstanceIdentity(identityGetJSONForServer(srv))
			if err == nil {
				t.Fatal("fetchInstanceIdentity error = nil, want non-nil")
			}
			var httpErr *httpStatusError
			if !errors.As(err, &httpErr) {
				t.Fatalf("error type = %T, want *httpStatusError", err)
			}
			if httpErr.code != tc.wantCode {
				t.Errorf("http status code = %d, want %d", httpErr.code, tc.wantCode)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

func TestFetchInstanceIdentity_MalformedJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "not-json")
	}))
	defer srv.Close()

	_, err := fetchInstanceIdentity(identityGetJSONForServer(srv))
	if err == nil {
		t.Fatal("fetchInstanceIdentity error = nil, want decoding error")
	}
	if !strings.Contains(err.Error(), "decoding /instance-identity/api/json response") {
		t.Errorf("error %q does not contain decoding fragment", err.Error())
	}
}

func TestFetchInstanceIdentity_BadPublicKey(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty publicKey", `{"publicKey":""}`, "empty publicKey"},
		{"invalid base64", `{"publicKey":"!!bad!!"}`, "decoding publicKey"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := fetchInstanceIdentity(identityGetJSONForServer(srv))
			if err == nil {
				t.Fatal("fetchInstanceIdentity error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestFetchInstanceIdentity_PropagatesTransportTimeout(t *testing.T) {
	timeoutErr := context.DeadlineExceeded
	getJSON := func(path string, out interface{}) error {
		return fmt.Errorf("requesting %s: %w", path, timeoutErr)
	}

	_, err := fetchInstanceIdentity(getJSON)
	if err == nil {
		t.Fatal("fetchInstanceIdentity error = nil, want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want wrapped context.DeadlineExceeded", err)
	}
}

func TestGetJSON_CapturesXJenkinsVersionHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Jenkins", "2.426.1")
		_, _ = w.Write([]byte(`{"jobs":[]}`))
	}))
	defer srv.Close()

	p := &jenkinsProbe{
		cfg:    jenkinsConfig{Endpoint: srv.URL},
		client: srv.Client(),
		entitySource: newEntitySource(
			"inst",
			"host",
			"80",
			func() (string, error) { return "", nil },
			func() string { return "" },
		),
	}

	var resp jobsResponse
	if err := p.getJSON(context.Background(), "/api/json", &resp); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if got, want := p.entitySource.currentVersion(), "2.426.1"; got != want {
		t.Errorf("currentVersion() = %q, want %q", got, want)
	}
}

func TestCollect_HTTPFailureEmitUpZero(t *testing.T) {
	cases := []struct {
		name string
		gen  func(w http.ResponseWriter)
	}{
		{"401", func(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }},
		{"500", func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }},
		{"invalid JSON", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "not-json")
		}},
		{"truncated body", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"jobs":[{"name":"a"`)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.gen(w)
			}))
			defer srv.Close()

			p := mustBuildJenkinsProbe(t, map[string]interface{}{"endpoint": srv.URL})
			points, err := p.Collect()
			if err != nil {
				t.Fatalf("Collect should not error on a down controller: %v", err)
			}

			var up float64 = -1
			for _, dp := range points {
				if dp.Name == metricUp {
					up = dp.Value
				}
			}
			if up != 0 {
				t.Errorf("senhub.jenkins.up = %v, want 0", up)
			}
		})
	}
}
