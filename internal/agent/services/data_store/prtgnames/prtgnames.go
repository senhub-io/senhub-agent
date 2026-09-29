// Package prtgnames keeps the channel names of one PRTG result unique.
package prtgnames

import (
	"fmt"
	"sort"
	"strings"
)

// Disambiguate returns the channel names of one PRTG result, made unique.
// PRTG keys a channel on its name and keeps one value per name, so two
// series whose display name did not carry what sets them apart (a scraped
// exporter's labels, a component index) silently collapsed into one
// channel. Colliding names get the tag values that differ inside the
// group, in key order: "Node CPU Seconds (cpu=0, mode=idle)". A group
// that no tag separates is numbered. tags[i] belongs to names[i].
func Disambiguate(names []string, tags []map[string]string) []string {
	out := append([]string(nil), names...)
	groups := map[string][]int{}
	for i, n := range names {
		groups[n] = append(groups[n], i)
	}
	for name, idx := range groups {
		if len(idx) < 2 {
			continue
		}
		keys := map[string]bool{}
		for _, i := range idx {
			for k := range tags[i] {
				keys[k] = true
			}
		}
		var differing []string
		for k := range keys {
			first := tags[idx[0]][k]
			for _, i := range idx[1:] {
				if tags[i][k] != first {
					differing = append(differing, k)
					break
				}
			}
		}
		sort.Strings(differing)
		seen := map[string]int{}
		for _, i := range idx {
			parts := make([]string, 0, len(differing))
			for _, k := range differing {
				if v := tags[i][k]; v != "" {
					parts = append(parts, k+"="+v)
				}
			}
			suffix := strings.Join(parts, ", ")
			seen[suffix]++
			if n := seen[suffix]; n > 1 || suffix == "" {
				if suffix != "" {
					suffix += ", "
				}
				suffix += fmt.Sprintf("#%d", n)
			}
			out[i] = name + " (" + suffix + ")"
		}
	}
	return out
}
