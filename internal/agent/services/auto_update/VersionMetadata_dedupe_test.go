package auto_update

import (
	"reflect"
	"testing"

	"github.com/ybbus/httpretry"

	"senhub-agent.go/internal/testUtils"
)

func TestIsAlias(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"latest", true},
		{"latest-beta", true},
		{"0.6.5", false},
		{"0.7.0-rc.6", false},
		{"", false},
	}
	for _, c := range cases {
		if got := (VersionMetadata{Name: c.name}).IsAlias(); got != c.want {
			t.Errorf("IsAlias(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDedupeVersions(t *testing.T) {
	cases := []struct {
		name string
		in   []VersionMetadata
		want []VersionMetadata
	}{
		{
			name: "alias before the release",
			in: []VersionMetadata{
				{Name: "latest-beta", Version: "0.7.0-rc.6"},
				{Name: "0.7.0-rc.6", Version: "0.7.0-rc.6"},
				{Name: "0.7.0-rc.5", Version: "0.7.0-rc.5"},
			},
			want: []VersionMetadata{
				{Name: "0.7.0-rc.6", Version: "0.7.0-rc.6"},
				{Name: "0.7.0-rc.5", Version: "0.7.0-rc.5"},
			},
		},
		{
			name: "alias after the release",
			in: []VersionMetadata{
				{Name: "0.7.0-rc.6", Version: "0.7.0-rc.6"},
				{Name: "latest-beta", Version: "0.7.0-rc.6"},
				{Name: "0.7.0-rc.5", Version: "0.7.0-rc.5"},
			},
			want: []VersionMetadata{
				{Name: "0.7.0-rc.6", Version: "0.7.0-rc.6"},
				{Name: "0.7.0-rc.5", Version: "0.7.0-rc.5"},
			},
		},
		{
			name: "alias only",
			in: []VersionMetadata{
				{Name: "latest", Version: "0.6.5"},
				{Name: "0.6.4", Version: "0.6.4"},
			},
			want: []VersionMetadata{
				{Name: "latest", Version: "0.6.5"},
				{Name: "0.6.4", Version: "0.6.4"},
			},
		},
		{
			name: "no alias",
			in: []VersionMetadata{
				{Name: "0.6.5", Version: "0.6.5"},
				{Name: "0.6.4", Version: "0.6.4"},
			},
			want: []VersionMetadata{
				{Name: "0.6.5", Version: "0.6.5"},
				{Name: "0.6.4", Version: "0.6.4"},
			},
		},
		{
			name: "stable and beta merged",
			in: []VersionMetadata{
				{Name: "latest", Version: "0.6.5"},
				{Name: "0.6.5", Version: "0.6.5"},
				{Name: "latest-beta", Version: "0.7.0-rc.6"},
				{Name: "0.7.0-rc.6", Version: "0.7.0-rc.6"},
				{Name: "0.6.5", Version: "0.6.5"},
			},
			want: []VersionMetadata{
				{Name: "0.6.5", Version: "0.6.5"},
				{Name: "0.7.0-rc.6", Version: "0.7.0-rc.6"},
			},
		},
		{
			name: "empty name is not an alias",
			in: []VersionMetadata{
				{Name: "", Version: "0.6.5"},
				{Name: "latest", Version: "0.6.5"},
			},
			want: []VersionMetadata{
				{Name: "", Version: "0.6.5"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DedupeVersions(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("DedupeVersions() = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestFetchAllVersionsKeepsTheNewestRelease(t *testing.T) {
	srv := testUtils.GetTestHTTPServerWithURLPath([]testUtils.TestHTTPServerURLConf{
		{URLPath: VERSION_METADATA_LIST_PATH, Method: "GET", StatusCode: 200, Body: `[{"name":"latest","version":"0.6.5"},{"name":"0.6.5","version":"0.6.5"},{"name":"0.6.4","version":"0.6.4"}]`},
		{URLPath: VERSION_METADATA_LIST_BETA_PATH, Method: "GET", StatusCode: 200, Body: `[{"name":"latest-beta","version":"0.7.0-rc.6"},{"name":"0.7.0-rc.6","version":"0.7.0-rc.6"},{"name":"0.7.0-rc.5","version":"0.7.0-rc.5"}]`},
	})
	defer srv.Server.Close()

	for _, includeBeta := range []bool{false, true} {
		versions, err := FetchAllVersions(httpretry.NewDefaultClient(), srv.URL, includeBeta, nil)
		if err != nil {
			t.Fatalf("FetchAllVersions(includeBeta=%v) error: %v", includeBeta, err)
		}
		counts := make(map[string]int)
		for _, v := range versions {
			if v.IsAlias() {
				t.Errorf("FetchAllVersions(includeBeta=%v) returned alias entry %+v", includeBeta, v)
			}
			counts[v.Version]++
		}
		want := []string{"0.6.5", "0.6.4"}
		if includeBeta {
			want = append(want, "0.7.0-rc.6", "0.7.0-rc.5")
		}
		if len(counts) != len(want) {
			t.Errorf("FetchAllVersions(includeBeta=%v) returned %d versions, want %d: %v", includeBeta, len(counts), len(want), counts)
		}
		for _, w := range want {
			if counts[w] != 1 {
				t.Errorf("FetchAllVersions(includeBeta=%v): version %s appears %d times, want exactly 1", includeBeta, w, counts[w])
			}
		}
	}
}
