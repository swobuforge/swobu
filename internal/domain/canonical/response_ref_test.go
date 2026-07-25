package canonical

import (
	"reflect"
	"testing"
)

func TestResponseIdentityDomainsHaveDistinctNominalTypes(t *testing.T) {
	if reflect.TypeOf(SwobuResponseID("")) == reflect.TypeOf(ResponsesResponseID("")) {
		t.Fatal("Swobu and provider response identities share a nominal type")
	}
}

func TestResponseIdentityConstructorsPreserveOpaqueBytes(t *testing.T) {
	const raw = "  opaque response id  "
	if got := NewSwobuResponseID(raw); string(got) != raw {
		t.Fatalf("Swobu response ID = %q, want exact %q", got, raw)
	}
	if got := NewResponsesResponseID(raw); string(got) != raw {
		t.Fatalf("provider response ID = %q, want exact %q", got, raw)
	}
}

func TestResponseRefBoundaryValidation(t *testing.T) {
	for name, ref := range map[string]ResponseRef{
		"empty":      {},
		"whitespace": {SwobuID: "   "},
	} {
		t.Run("selector/"+name, func(t *testing.T) {
			if err := ref.ValidatePreviousResponseSelector(); err == nil {
				t.Fatal("empty previous-response selector accepted")
			}
		})
		t.Run("committed/"+name, func(t *testing.T) {
			if err := ref.ValidateCommittedResponse(); err == nil {
				t.Fatal("empty committed response accepted")
			}
		})
	}
}

func TestResponsesContinuationValidateBound(t *testing.T) {
	valid := ResponsesContinuation{ProviderResponseID: "provider_resp_789", TargetID: "target-a", TargetVersion: 7}
	if err := valid.ValidateBound(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ResponsesContinuation){
		"provider ID":    func(ref *ResponsesContinuation) { ref.ProviderResponseID = "" },
		"target ID":      func(ref *ResponsesContinuation) { ref.TargetID = "" },
		"target version": func(ref *ResponsesContinuation) { ref.TargetVersion = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := candidate.ValidateBound(); err == nil {
				t.Fatalf("invalid native ref accepted: %#v", candidate)
			}
		})
	}
}
