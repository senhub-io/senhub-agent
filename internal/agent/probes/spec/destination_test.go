package spec

import (
	"reflect"
	"testing"
)

// A list of addresses is a destination as much as a single one: a caller
// that keeps the main URL and adds a fallback of its own must not take the
// stored credential there.
func TestDestinationPathsIncludePluralKeys(t *testing.T) {
	params := []ParamSpec{
		{Key: "director", Kind: KindBlock, Fields: []ParamSpec{
			{Key: "url", Kind: KindString},
			{Key: "fallback_urls", Kind: KindStringList},
			{Key: "password", Kind: KindString, Secret: true},
		}},
		{Key: "backup_hosts", Kind: KindStringList},
		{Key: "replica_addresses", Kind: KindStringList},
		{Key: "timeout", Kind: KindInt},
	}
	want := []string{"director.url", "director.fallback_urls", "backup_hosts", "replica_addresses"}
	if got := DestinationPaths(params); !reflect.DeepEqual(got, want) {
		t.Fatalf("DestinationPaths = %v, want %v", got, want)
	}
}
