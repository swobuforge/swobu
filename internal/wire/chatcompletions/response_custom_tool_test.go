package chatcompletions

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestCustomToolResponseRoundTrip(t *testing.T) {
	decl := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "apply_patch"), "", canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar"}`)))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, decl)}})
	callID, _ := canonical.NewToolCallID("call_1")
	call, _ := canonical.NewToolCallItem(callID, decl.Key(), canonical.NewTextToolInput("patch contents"))
	output := canonicaltest.Response(t, "resp_1", "m", []canonical.CanonicalItem{call}, "stop")
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, output)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := decodeResponseBuffered(context.Background(), request, encoded.Document.RawBytes(), "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := projected.Items()[0].ToolCall()
	text, ok := got.Input().Text()
	if !ok || text != "patch contents" || got.Tool().String() != decl.Key().String() {
		t.Fatalf("projected tool call=%#v", got)
	}
}
