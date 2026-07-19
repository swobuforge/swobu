package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommittingResponseWriterOwnsActualCommitState(t *testing.T) {
	rr := httptest.NewRecorder()
	writer := &committingResponseWriter{ResponseWriter: rr}

	if writer.committed {
		t.Fatal("new writer should not be committed")
	}
	writer.WriteHeader(http.StatusOK)
	if !writer.committed {
		t.Fatal("write header should mark writer committed")
	}

	writer = &committingResponseWriter{ResponseWriter: rr}
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatalf("write returned error: %v", err)
	}
	if !writer.committed {
		t.Fatal("write should mark writer committed")
	}
}
