package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/exchange"
)

func TestCommittingResponseWriter_MarksExchangeCommitGate(t *testing.T) {
	gate := exchange.NewCommitGate()
	rr := httptest.NewRecorder()
	writer := &committingResponseWriter{ResponseWriter: rr, gate: gate}

	if gate.Committed() {
		t.Fatal("new gate should not be committed")
	}
	writer.WriteHeader(http.StatusOK)
	if !gate.Committed() {
		t.Fatal("write header should mark gate committed")
	}

	gate = exchange.NewCommitGate()
	writer = &committingResponseWriter{ResponseWriter: rr, gate: gate}
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatalf("write returned error: %v", err)
	}
	if !gate.Committed() {
		t.Fatal("write should mark gate committed")
	}
}
