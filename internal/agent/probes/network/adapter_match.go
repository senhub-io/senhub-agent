package network

// matchPDHInstance returns the PDH "Network Interface" instance whose
// normalized form is exactly the normalized WMI adapter name. The previous
// Contains-style predicate let a short adapter name substring-match a longer
// sibling on multi-NIC hosts ("Ethernet" claimed "Ethernet 2" when the
// suffixed instance enumerated first), attaching one adapter's IP metadata to
// another adapter's counters (#643). normalizeAdapterName already folds the
// PDH/WMI spelling differences (#640), so equality is the correct predicate.
func matchPDHInstance(pdhInstances []string, wmiAdapterName string) (string, bool) {
	want := normalizeAdapterName(wmiAdapterName)
	for _, inst := range pdhInstances {
		if normalizeAdapterName(inst) == want {
			return inst, true
		}
	}
	return "", false
}

// unmatchedPDHInstances lists the enumerated PDH instances that no WMI
// adapter claimed, preserving enumeration order. An unclaimed instance still
// carries real traffic counters; dropping it because IP metadata could not be
// attached lost the whole series (#644), so the caller emits these without
// the enrichment.
func unmatchedPDHInstances[V any](pdhInstances []string, matched map[string]V) []string {
	unmatched := make([]string, 0, len(pdhInstances))
	for _, inst := range pdhInstances {
		if _, ok := matched[inst]; !ok {
			unmatched = append(unmatched, inst)
		}
	}
	return unmatched
}
