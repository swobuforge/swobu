package responses

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestNamespacedFunctionCallUsesAttemptScopedFlatAddress(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"tools":[{"type":"namespace","name":"crm","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}],
		"input":[{"type":"function_call","namespace":"crm","name":"lookup","call_id":"call_1","arguments":{}}]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
		protocolkind.Responses, "application/json", nil, raw, carrier.Meta{},
	))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := decoded.Request.Request.Items()[1].ToolCall()
	if !ok || call.Tool().Namespace() != "crm" || call.Tool().Name() != "lookup" {
		t.Fatalf("namespaced call identity = %#v", call.Tool())
	}
	names, _, err := provider.BuildAttemptToolNames(decoded.Request.Request)
	if err != nil {
		t.Fatal(err)
	}
	wireName, err := names.WireName(call.Tool())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decoded.Request.Request, ToolNames: names}, delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded.RawBytes())
	if strings.Contains(body, `"namespace"`) || !strings.Contains(body, `"name":"`+wireName+`"`) {
		t.Fatalf("projected flat call address was not preserved: %s", body)
	}
}
