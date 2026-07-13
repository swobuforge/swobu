package chatcompletions

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncodeChatCompletionsTool_RejectsUnsupportedKindsWithKindDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tool     canonical.ToolDecl
		wantKind string
	}{
		{
			name:     "capability",
			tool:     canonical.NewCapabilityToolDecl("tool_search", canonical.NewToolCapability("tool_search"), canonical.EmptyToolCapabilityConfig()),
			wantKind: "tool_search",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := encodeChatCompletionsTool(tc.tool, nil, "", 0)
			if err == nil {
				t.Fatal("expected encodeChatCompletionsTool to reject unsupported kind")
			}
			var compatErr canonical.Error
			if !errors.As(err, &compatErr) {
				t.Fatalf("error = %T, want canonical.Error", err)
			}
			if compatErr.Code != canonical.ErrorCodeUnsupportedOperation {
				t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeUnsupportedOperation)
			}
			if !strings.Contains(compatErr.Message, tc.wantKind) {
				t.Fatalf("error message = %q, want kind detail %q", compatErr.Message, tc.wantKind)
			}
		})
	}
}
