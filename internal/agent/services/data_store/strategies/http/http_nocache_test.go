package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoStoreMarksAnswersUnreusable(t *testing.T) {
	h := NoStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/k/prtg/metrics/cpu", nil))

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("a measurement must not be reusable: Cache-Control = %q, want %q", got, "no-store")
	}
}

// A handler that states its own policy runs after the middleware and must
// win: the embedded console assets ask for revalidation rather than
// no-store, and that is the behaviour to keep.
func TestNoStoreYieldsToAHandlerThatSetsItsOwn(t *testing.T) {
	h := NoStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/web/k/assets/js/base.js", nil))

	if got := rec.Header().Get("Cache-Control"); got != "no-cache, must-revalidate" {
		t.Fatalf("the handler's own policy must stand: Cache-Control = %q", got)
	}
}
