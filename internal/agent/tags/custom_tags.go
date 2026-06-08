package tags

import "sort"

// MapToTags converts a string map (as carried by agent global_tags / probe
// custom_tags config) into an ordered Tag slice. Keys are sorted for
// deterministic output. Returns nil for an empty map.
func MapToTags(m map[string]string) []Tag {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Tag, 0, len(keys))
	for _, k := range keys {
		out = append(out, Tag{Key: k, Value: m[k]})
	}
	return out
}

// MergeTags overlays tag layers left-to-right: a later layer's value wins on
// a key conflict (last-layer-wins). Used to apply the configured-tag priority
// probe custom_tags > agent global_tags > built-in probe tags by passing the
// layers in that order: MergeTags(builtin, global, custom). Order of first
// appearance of each key is preserved.
func MergeTags(layers ...[]Tag) []Tag {
	idx := make(map[string]int) // key -> position in out
	out := make([]Tag, 0)
	for _, layer := range layers {
		for _, t := range layer {
			if pos, ok := idx[t.Key]; ok {
				// keep position + privacy of the first appearance, override value
				out[pos].Value = t.Value
				if t.Private {
					out[pos].Private = true
				}
				continue
			}
			idx[t.Key] = len(out)
			out = append(out, t)
		}
	}
	return out
}
