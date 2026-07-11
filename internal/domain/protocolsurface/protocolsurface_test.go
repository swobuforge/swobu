package protocolsurface

import "testing"

func TestDeliveryValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		d       Delivery
		wantErr bool
	}{
		{
			name:    "buffered",
			d:       BufferedDelivery(),
			wantErr: false,
		},
		{
			name:    "streaming sse",
			d:       StreamingDelivery(FramingSSE),
			wantErr: false,
		},
		{
			name:    "streaming websocket",
			d:       StreamingDelivery(FramingWebSocket),
			wantErr: false,
		},
		{
			name:    "streaming ndjson",
			d:       StreamingDelivery(FramingNDJSON),
			wantErr: false,
		},
		{
			name:    "buffered with framing",
			d:       Delivery{Variant: DeliveryVariantBuffered, Framing: FramingSSE},
			wantErr: true,
		},
		{
			name:    "streaming without framing",
			d:       Delivery{Variant: DeliveryVariantStreaming, Framing: FramingNone},
			wantErr: true,
		},
		{
			name:    "invalid variant",
			d:       Delivery{Variant: DeliveryVariant("unknown"), Framing: FramingNone},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.d.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate() expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestDeliveryIsStreaming(t *testing.T) {
	t.Parallel()

	if BufferedDelivery().IsStreaming() {
		t.Fatal("buffered delivery should not be streaming")
	}
	if !StreamingDelivery(FramingSSE).IsStreaming() {
		t.Fatal("streaming delivery should be streaming")
	}
}

func TestCodecIDString(t *testing.T) {
	t.Parallel()

	if got := CodecIDResponsesStreamSSE.String(); got != "responses.streaming.sse" {
		t.Fatalf("CodecID.String() = %q", got)
	}
}
