package tags

import (
	"reflect"
	"testing"
)

func TestMapToTags(t *testing.T) {
	if got := MapToTags(nil); got != nil {
		t.Errorf("MapToTags(nil) = %v, want nil", got)
	}
	got := MapToTags(map[string]string{"b": "2", "a": "1"})
	want := []Tag{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MapToTags = %v, want sorted %v", got, want)
	}
}

func TestMergeTags_Priority(t *testing.T) {
	builtin := []Tag{{Key: "site", Value: "builtin"}, {Key: "host", Value: "h1"}}
	global := []Tag{{Key: "site", Value: "global"}, {Key: "region", Value: "west"}}
	custom := []Tag{{Key: "site", Value: "custom"}}

	// Priority custom > global > built-in: pass layers in that order.
	got := MergeTags(builtin, global, custom)

	byKey := map[string]string{}
	for _, tg := range got {
		byKey[tg.Key] = tg.Value
	}
	if byKey["site"] != "custom" {
		t.Errorf("site = %q, want custom (custom_tags wins)", byKey["site"])
	}
	if byKey["region"] != "west" {
		t.Errorf("region = %q, want west (global adds)", byKey["region"])
	}
	if byKey["host"] != "h1" {
		t.Errorf("host = %q, want h1 (built-in preserved)", byKey["host"])
	}
	// First-appearance order preserved: site, host, region.
	wantOrder := []string{"site", "host", "region"}
	for i, tg := range got {
		if tg.Key != wantOrder[i] {
			t.Errorf("order[%d] = %q, want %q", i, tg.Key, wantOrder[i])
		}
	}
}

func TestMergeTags_PreservesPrivate(t *testing.T) {
	builtin := []Tag{{Key: "secret", Value: "v", Private: true}}
	override := []Tag{{Key: "secret", Value: "v2"}}
	got := MergeTags(builtin, override)
	if len(got) != 1 || !got[0].Private || got[0].Value != "v2" {
		t.Errorf("merged = %+v, want one private secret=v2", got)
	}
}
