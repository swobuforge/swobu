package responses

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_RejectsNonResponseJSON(t *testing.T) {
	_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, []byte(`{"code":500,"message":"not a response"}`), "ex_invalid_shape", nil)
	if err == nil {
		t.Fatal("decodeResponseBuffered error = nil, want invalid response shape")
	}
	var swobuErr canonical.Error
	if !errors.As(err, &swobuErr) || swobuErr.Code != canonical.ErrorCodeInternal {
		t.Fatalf("decodeResponseBuffered error = %#v, want canonical internal error", err)
	}
}
