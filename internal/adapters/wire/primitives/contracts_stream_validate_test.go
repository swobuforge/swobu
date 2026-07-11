package core

import (
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestValidateResponseSSEWireStream(t *testing.T) {
	wire := WireStream{
		Kind:     WireKindResponseStream,
		Protocol: protocolkind.Responses,
		Body:     io.NopCloser(strings.NewReader("")),
		Framing:  FramingSSE,
	}
	if err := ValidateResponseSSEWireStream(wire, protocolkind.Responses); err != nil {
		t.Fatalf("ValidateResponseSSEWireStream returned error: %v", err)
	}
}

func TestValidateResponseSSEWireStream_RejectsInvalidShape(t *testing.T) {
	tests := []struct {
		name string
		wire WireStream
	}{
		{
			name: "wrong kind",
			wire: WireStream{
				Kind:     WireKindRequest,
				Protocol: protocolkind.Responses,
				Body:     io.NopCloser(strings.NewReader("")),
				Framing:  FramingSSE,
			},
		},
		{
			name: "wrong protocol",
			wire: WireStream{
				Kind:     WireKindResponseStream,
				Protocol: protocolkind.Messages,
				Body:     io.NopCloser(strings.NewReader("")),
				Framing:  FramingSSE,
			},
		},
		{
			name: "wrong framing",
			wire: WireStream{
				Kind:     WireKindResponseStream,
				Protocol: protocolkind.Responses,
				Body:     io.NopCloser(strings.NewReader("")),
				Framing:  FramingNDJSON,
			},
		},
		{
			name: "missing body",
			wire: WireStream{
				Kind:     WireKindResponseStream,
				Protocol: protocolkind.Responses,
				Framing:  FramingSSE,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateResponseSSEWireStream(tt.wire, protocolkind.Responses); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}
