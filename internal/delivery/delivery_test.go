package delivery

import "testing"

func TestDeliveryValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		d       Delivery
		wantErr bool
	}{
		{name: "buffered none", d: BufferedDelivery()},
		{name: "streaming sse", d: StreamingDelivery(FramingSSE)},
		{name: "streaming websocket", d: StreamingDelivery(FramingWebSocket)},
		{name: "streaming ndjson", d: StreamingDelivery(FramingNDJSON)},
		{name: "buffered sse invalid", d: Delivery{Mode: Buffered, Framing: FramingSSE}, wantErr: true},
		{name: "buffered websocket invalid", d: Delivery{Mode: Buffered, Framing: FramingWebSocket}, wantErr: true},
		{name: "buffered ndjson invalid", d: Delivery{Mode: Buffered, Framing: FramingNDJSON}, wantErr: true},
		{name: "streaming none invalid", d: Delivery{Mode: Streaming, Framing: FramingNone}, wantErr: true},
		{name: "invalid mode", d: Delivery{Mode: Mode(99), Framing: FramingNone}, wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.d.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate() expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestDeliveryValidate_ExhaustiveFiniteDomain(t *testing.T) {
	t.Parallel()

	modes := []Mode{Buffered, Streaming, Mode(0), Mode(99)}
	framings := []Framing{FramingNone, FramingSSE, FramingWebSocket, FramingNDJSON, Framing("invalid")}

	for _, mode := range modes {
		for _, framing := range framings {
			d := Delivery{Mode: mode, Framing: framing}
			err := d.Validate()
			wantErr := true

			if mode == Buffered && framing == FramingNone {
				wantErr = false
			}
			if mode == Streaming && (framing == FramingSSE || framing == FramingWebSocket || framing == FramingNDJSON) {
				wantErr = false
			}

			if wantErr && err == nil {
				t.Fatalf("expected error for mode=%q framing=%q", mode, framing)
			}
			if !wantErr && err != nil {
				t.Fatalf("unexpected error for mode=%q framing=%q: %v", mode, framing, err)
			}
		}
	}
}
