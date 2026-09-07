package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSONReportsAnEncodingFailureInsteadOfAnEmptyBody(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]interface{}{"bad": map[interface{}]interface{}{"k": "v"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "could not be encoded") {
		t.Fatalf("body must name the problem, got %q", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]string{"status": "success"})
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "success") {
		t.Fatalf("a good value must pass through: %d %q", rec.Code, rec.Body.String())
	}
}
