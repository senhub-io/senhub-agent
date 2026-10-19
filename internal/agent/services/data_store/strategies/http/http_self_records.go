package http

import (
	"net/http"
	"sort"
	"time"

	"senhub-agent.go/internal/agent/services/exporthook"
)

type selfRecordJSON struct {
	Name        string            `json:"name"`
	Unit        string            `json:"unit"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Value       float64           `json:"value"`
	Attributes  map[string]string `json:"attributes"`
}

// selfRecordsSnapshot returns what the registered providers add to the
// agent's own metrics, in a stable order so a poller sees no churn.
func selfRecordsSnapshot(now time.Time) []selfRecordJSON {
	recs := exporthook.SelfRecords(now)
	out := make([]selfRecordJSON, 0, len(recs))
	for _, r := range recs {
		attrs := r.Attributes
		if attrs == nil {
			attrs = map[string]string{}
		}
		out = append(out, selfRecordJSON{r.Name, r.Unit, r.Type, r.Description, r.Value, attrs})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return attrKey(out[i].Attributes) < attrKey(out[j].Attributes)
	})
	return out
}

func attrKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := ""
	for _, k := range keys {
		s += k + "=" + m[k] + "\x00"
	}
	return s
}

func (h *HTTPSyncStrategy) handleInfoSelfRecords(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"records": selfRecordsSnapshot(time.Now())})
}
