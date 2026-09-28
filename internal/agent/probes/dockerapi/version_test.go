package dockerapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
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
	if got := v.URL(client, "/containers/json"); got != "http://localhost/v1.44/containers/json" {
		t.Errorf("URL = %s", got)
	}
	v.URL(client, "/info")
	if asked != 1 {
		t.Errorf("the daemon was asked %d times, want once", asked)
	}
	v.Forget()
	v.URL(client, "/info")
	if asked != 2 {
		t.Errorf("after Forget the daemon must be asked again; asked %d times", asked)
	}
}

func TestURLFallsBackWhenTheDaemonCannotBeAsked(t *testing.T) {
	client := &http.Client{Transport: failing{}}
	var v Version
	if got := v.URL(client, "/info"); got != "http://localhost/v"+Highest+"/info" {
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
