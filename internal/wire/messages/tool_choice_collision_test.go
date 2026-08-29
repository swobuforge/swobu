package messages

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMessagesTreatsPostAllocationWireNameCollisionAsInternalInvariant(t *testing.T) {
	function := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "request/web_search"),
		"", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
	)
	key := function.Key()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, function, canonical.NewWebSearchDeclaration()),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key)),
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}

	_, err = CompileProviderRequestDocument(request, names, delivery.BufferedDelivery(), nil, "", CompileOptions{Lowering: DefaultLowering()})
	var canonicalError canonical.Error
	if !errors.As(err, &canonicalError) || canonicalError.Code != canonical.ErrorCodeInternal {
		t.Fatalf("error = %T %v, want internal allocator invariant failure", err, err)
	}
}
