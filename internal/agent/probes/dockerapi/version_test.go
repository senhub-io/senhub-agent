package dockerapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestChooseStaysInsideWhatTheDaemonServes(t *testing.T) {
	cases := []struct{ name, max, min, want string }{
		{"engine 29 refuses below 1.44", "1.52", "1.44", "1.44"},
		{"engine 24 serves up to 1.43", "1.43", "1.12", "1.43"},
		{"engine 20.10 serves up to 1.41", "1.41", "1.12", "1.41"},
		{"a future engine raising its minimum", "1.60", "1.50", "1.50"},
		{"no minimum reported", "1.52", "", "1.44"},
		{"numeric, not lexical", "1.9", "", "1.9"},
	}
	for _, c := range cases {
		if got := Choose(c.max, c.min); got != c.want {
			t.Errorf("%s: Choose(%s, %s) = %s, want %s", c.name, c.max, c.min, got, c.want)
		}
	}
}

func TestURLNegotiatesOnceThenReuses(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			asked++
			_, _ = w.Write([]byte(`{"ApiVersion":"1.52","MinAPIVersion":"1.44"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	target, _ := url.Parse(srv.URL)
	client := &http.Client{Transport: rewrite{target}}

	var v Version
	if got := v.URL(context.Background(), client, "/containers/json"); got != "http://localhost/v1.44/containers/json" {
		t.Errorf("URL = %s", got)
	}
	v.URL(context.Background(), client, "/info")
	if asked != 1 {
		t.Errorf("the daemon was asked %d times, want once", asked)
	}
	v.Forget()
	v.URL(context.Background(), client, "/info")
	if asked != 2 {
		t.Errorf("after Forget the daemon must be asked again; asked %d times", asked)
	}
}

func TestURLFallsBackWhenTheDaemonCannotBeAsked(t *testing.T) {
	client := &http.Client{Transport: failing{}}
	var v Version
	if got := v.URL(context.Background(), client, "/info"); got != "http://localhost/v"+Highest+"/info" {
		t.Errorf("URL = %s, want the highest known version", got)
	}
}

type rewrite struct{ to *url.URL }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	c.URL.Scheme, c.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(c)
}

type failing struct{}

func (failing) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrServerClosed
}

// A daemon that stopped answering must not hold the negotiation past its
// context, and the interrupted attempt must not be remembered as the
// version: the next call asks again.
func TestNegotiationEndsWithItsContextAndIsNotRemembered(t *testing.T) {
	release := make(chan struct{})
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if asked == 1 {
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		_, _ = w.Write([]byte(`{"ApiVersion":"1.43","MinAPIVersion":"1.12"}`))
	}))
	defer srv.Close()
	defer close(release)
	target, _ := url.Parse(srv.URL)
	client := &http.Client{Transport: rewrite{target}}

	var v Version
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	started := time.Now()
	if got := v.URL(ctx, client, "/info"); got != "http://localhost/v"+Highest+"/info" {
		t.Errorf("URL = %s, want the highest known version while the daemon is silent", got)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("the negotiation took %v after its context was cancelled", elapsed)
	}
	if got := v.URL(context.Background(), client, "/info"); got != "http://localhost/v1.43/info" {
		t.Errorf("URL = %s, want the version the daemon reports once it answers", got)
	}
}
