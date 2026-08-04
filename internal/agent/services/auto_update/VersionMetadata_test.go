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

// The release server publishes each channel with an alias record first,
// carrying the resolved version:
//
//	stable: [{latest, 0.5.3}, {0.5.3, 0.5.3}, {0.5.2, 0.5.2}, …]
//	beta:   [{latest-beta, 0.5.3-beta}, {0.5.3-beta, 0.5.3-beta}, …]
//
// FetchAllVersions dedups by version keeping the first record, so the
// concrete 0.5.3 entry is dropped and the alias one survives. Discarding
// alias records in GetLatestVersion then made the newest stable release
// invisible, and the beta — whose alias is named "latest-beta" and so
// escaped the same filter — won. An include_beta host stayed pinned to
// its beta forever (#730).
func TestGetLatestVersion_ChannelAliasRecordIsNotDiscarded(t *testing.T) {
	merged := []VersionMetadata{
		{Name: "latest", Version: "0.5.3"},
		{Name: "0.5.2", Version: "0.5.2"},
		{Name: "latest-beta", Version: "0.5.3-beta"},
		{Name: "0.5.2-beta", Version: "0.5.2-beta"},
	}

	latest := GetLatestVersion(merged)
	if latest == nil {
		t.Fatal("GetLatestVersion() = nil, want the newest release")
	}
	if latest.Version != "0.5.3" {
		t.Fatalf("GetLatestVersion() = %q, want %q — a stable release must not be hidden by the record that names its channel",
			latest.Version, "0.5.3")
	}
}

// A beta genuinely ahead of the newest stable must still win, otherwise
// opting into betas would stop delivering them.
func TestGetLatestVersion_NewerBetaStillWins(t *testing.T) {
	merged := []VersionMetadata{
		{Name: "latest", Version: "0.5.3"},
		{Name: "latest-beta", Version: "0.5.4-beta"},
	}

	latest := GetLatestVersion(merged)
	if latest == nil || latest.Version != "0.5.4-beta" {
		t.Fatalf("GetLatestVersion() = %v, want 0.5.4-beta", latest)
	}
}

// An alias record whose version is the literal channel name is not a
// version; it must be ignored rather than crash or win.
func TestGetLatestVersion_UnparseableRecordIsIgnored(t *testing.T) {
	merged := []VersionMetadata{
		{Name: "latest", Version: "latest"},
		{Name: "0.5.3", Version: "0.5.3"},
	}

	latest := GetLatestVersion(merged)
	if latest == nil || latest.Version != "0.5.3" {
		t.Fatalf("GetLatestVersion() = %v, want 0.5.3", latest)
	}
}
