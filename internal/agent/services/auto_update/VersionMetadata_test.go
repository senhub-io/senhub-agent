package auto_update

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchVersionList_Non200ReturnsHTTPError guards #585/#586: a registry URL
// that 404s (e.g. a doubled `/releases` path from a mis-scaffolded config) must
// surface the HTTP status, not the cryptic JSON error that came from decoding
// the "404 page not found" body's leading number.
func TestFetchVersionList_Non200ReturnsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // 404 + "404 page not found\n"
	}))
	defer srv.Close()

	if _, err := fetchVersionList(srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error on HTTP 404, got nil")
	} else if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("error should mention HTTP 404, got: %v", err)
	} else if strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error must not be a JSON unmarshal error, got: %v", err)
	}
}

// TestFetchVersionList_OKParsesArray confirms the happy path still decodes the
// [{name, version}] array the registry serves.
func TestFetchVersionList_OKParsesArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"latest","version":"0.4.1"},{"name":"0.4.1","version":"0.4.1"}]`))
	}))
	defer srv.Close()

	list, err := fetchVersionList(srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 || list[0].Name != "latest" || list[0].Version != "0.4.1" {
		t.Errorf("unexpected list: %+v", list)
	}
}
