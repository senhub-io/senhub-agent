package hostiface

type macOwnerCandidate struct {
	name string
	mac  string
}

func dedupeOwnedMACs(candidates []macOwnerCandidate) map[string]string {
	out := make(map[string]string)
	drop := make(map[string]bool)
	for _, c := range candidates {
		if c.mac == "" {
			continue
		}
		if drop[c.mac] {
			continue
		}
		if _, exists := out[c.mac]; exists {
			delete(out, c.mac)
			drop[c.mac] = true
			continue
		}
		out[c.mac] = c.name
	}
	return out
}

func allZeroMAC(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
