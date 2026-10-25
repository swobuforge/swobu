package ledger

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestActivityCursorCanonicalScopeAndKindBinding(t *testing.T) {
	settled := time.Date(2026, 10, 13, 12, 0, 0, 1, time.UTC)
	cursor := EncodeActivityCursor("research", "mcp_operation", settled, "mcp_z")
	gotTime, identity, err := DecodeActivityCursor(cursor, "research", "mcp_operation")
	if err != nil || !gotTime.Equal(settled) || identity != "mcp_z" {
		t.Fatal("canonical Activity cursor did not round trip")
	}
	for _, raw := range []string{"", "invalid", cursor + "=", base64.RawURLEncoding.EncodeToString([]byte(`{"workspace":"research","kind":"mcp_operation","settled":"2026-10-13T12:00:00Z","identity":"mcp_z","extra":true}`)), base64.RawURLEncoding.EncodeToString([]byte(`{"workspace":"research","kind":"mcp_operation","settled":"2026-10-13T12:00:00Z","identity":"mcp_z","identity":"mcp_a"}`))} {
		if _, _, err := DecodeActivityCursor(raw, "research", "mcp_operation"); err == nil {
			t.Fatal("malformed/noncanonical cursor accepted")
		}
	}
	for _, binding := range []struct{ workspace, kind string }{{"coding", "mcp_operation"}, {"", "mcp_operation"}, {"research", "exchange"}, {"research", "unsupported"}} {
		if _, _, err := DecodeActivityCursor(cursor, binding.workspace, binding.kind); err == nil {
			t.Fatal("foreign cursor scope/kind accepted")
		}
	}
}
