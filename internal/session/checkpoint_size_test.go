package session

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestValidateRequestSizeRejectsKnownOversizeMaterialBeforeCapture(t *testing.T) {
	const limit = int64(1024)
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart(strings.Repeat("x", int(limit)))})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{message}})
	if err := ValidateRequestSize(request, limit); err == nil {
		t.Fatal("oversize known request material passed checkpoint preflight")
	}
}
