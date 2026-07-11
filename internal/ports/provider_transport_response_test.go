package ports

import (
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestProviderTransportResponseValidate_ExactlyOneCarrierRequired(t *testing.T) {
	tests := []struct {
		name    string
		input   ProviderTransportResponse
		wantErr bool
	}{
		{
			name:    "none invalid",
			input:   ProviderTransportResponse{},
			wantErr: true,
		},
		{
			name: "document only valid",
			input: ProviderTransportResponse{
				Document: []byte(`{"ok":true}`),
			},
		},
		{
			name: "stream only valid",
			input: ProviderTransportResponse{
				Stream: io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n")),
			},
		},
		{
			name: "envelope only valid",
			input: ProviderTransportResponse{
				Envelope: canonical.NewSliceEventReader(canonical.EventSequence{}),
			},
		},
		{
			name: "document and stream invalid",
			input: ProviderTransportResponse{
				Document: []byte(`{"ok":true}`),
				Stream:   io.NopCloser(strings.NewReader("x")),
			},
			wantErr: true,
		},
		{
			name: "document and envelope invalid",
			input: ProviderTransportResponse{
				Document: []byte(`{"ok":true}`),
				Envelope: canonical.NewSliceEventReader(canonical.EventSequence{}),
			},
			wantErr: true,
		},
		{
			name: "stream and envelope invalid",
			input: ProviderTransportResponse{
				Stream:   io.NopCloser(strings.NewReader("x")),
				Envelope: canonical.NewSliceEventReader(canonical.EventSequence{}),
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate() expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}
