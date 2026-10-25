package core

import (
	"io"
	"strings"
	"testing"
)

func TestBoundedSSEReaderUsesCallerLineBudget(t *testing.T) {
	payload := strings.Repeat("x", (1<<20)+1024)
	wire := "data: " + payload + "\n\n"
	ordinary := NewSSEReader(io.NopCloser(strings.NewReader(wire)))
	defer ordinary.Close()
	if _, err := ordinary.Next(t.Context()); err == nil {
		t.Fatal("default SSE line budget changed")
	}
	bounded := NewBoundedSSEReader(io.NopCloser(strings.NewReader(wire)), 2<<20)
	defer bounded.Close()
	event, err := bounded.Next(t.Context())
	if err != nil || event.Data != payload {
		t.Fatalf("caller-bounded frame changed: length=%d err=%v", len(event.Data), err)
	}
	tooSmall := NewBoundedSSEReader(io.NopCloser(strings.NewReader(wire)), 128<<10)
	defer tooSmall.Close()
	if _, err := tooSmall.Next(t.Context()); err == nil {
		t.Fatal("caller SSE line budget bypassed")
	}
}
