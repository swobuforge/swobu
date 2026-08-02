package responses

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestHostedDiscoveryNullCallIDRoundTripsWithoutSyntheticLeak(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"tools":[{"type":"tool_search","execution":"server","parameters":{"type":"object"}}],
		"input":[
			{"type":"tool_search_call","execution":"server","call_id":null,"arguments":{}},
			{"type":"tool_search_output","execution":"server","call_id":null,"status":"completed","tools":[{"type":"function","name":"loaded","parameters":{"type":"object"}}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
		protocolkind.Responses, "application/json", nil, raw, carrier.Meta{},
	))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded.RawBytes())
	if strings.Count(body, `"call_id":null`) != 2 {
		t.Fatalf("hosted null IDs were not preserved: %s", body)
	}
	if strings.Contains(body, "responses_hosted_") {
		t.Fatalf("synthetic discovery ID leaked to wire: %s", body)
	}
}

func TestClientDiscoveryStillRequiresWireCallID(t *testing.T) {
	raw := []byte(`{"model":"m","tools":[{"type":"tool_search","execution":"client","parameters":{"type":"object"}}],"input":[{"type":"tool_search_call","execution":"client","call_id":null,"arguments":{}}]}`)
	if _, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
		protocolkind.Responses, "application/json", nil, raw, carrier.Meta{},
	)); err == nil {
		t.Fatal("client discovery accepted a null wire call ID")
	}
}
