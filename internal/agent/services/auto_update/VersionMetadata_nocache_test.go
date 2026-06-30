package auto_update

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestVersionListFetch_SendsNoCache pins #599: the version-list fetches must
// ask intermediaries to revalidate, so a freshly published stable release is
// seen immediately instead of a cached pre-release list.
func TestVersionListFetch_SendsNoCache(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		fn   func(*http.Client, string) ([]VersionMetadata, error)
	}{
		{"stable", VERSION_METADATA_LIST_PATH, fetchVersionList},
		{"beta", VERSION_METADATA_LIST_BETA_PATH, fetchVersionListBeta},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotCC, gotPragma string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotCC = r.Header.Get("Cache-Control")
				gotPragma = r.Header.Get("Pragma")
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `[{"name":"latest","version":"0.4.2"},{"name":"0.4.2","version":"0.4.2"}]`)
			}))
			defer srv.Close()

			if _, err := tc.fn(srv.Client(), srv.URL); err != nil {
				t.Fatalf("fetch %s: %v", tc.name, err)
			}
			if gotCC != "no-cache" {
				t.Errorf("%s Cache-Control = %q, want no-cache", tc.name, gotCC)
			}
			if gotPragma != "no-cache" {
				t.Errorf("%s Pragma = %q, want no-cache", tc.name, gotPragma)
			}
		})
	}
}
