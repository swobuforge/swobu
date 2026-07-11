package sse

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeStrictJSON_ReportsJSONPointerForUnknownField(t *testing.T) {
	t.Parallel()

	var dto struct {
		Model string `json:"model"`
	}
	err := DecodeStrictJSON(json.RawMessage(`{"model":"m","a/b~c":true}`), &dto, "messages request")
	if err == nil {
		t.Fatal("expected error")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if got := compatErr.Details["json_field"]; got != "a/b~c" {
		t.Fatalf("json_field = %q, want %q", got, "a/b~c")
	}
	if got := compatErr.Details["json_pointer"]; got != "/a~1b~0c" {
		t.Fatalf("json_pointer = %q, want %q", got, "/a~1b~0c")
	}
}
