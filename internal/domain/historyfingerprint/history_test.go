package historyfingerprint

import (
	"bytes"
	"testing"
)

func TestOrderedHistoryComposition(t *testing.T) {
	request := mustRequest(t, "responses", "request")
	response := mustResponse(t, "responses", "response")
	first, err := Advance(nil, request, response)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Advance(nil, request, response)
	if err != nil || again != first {
		t.Fatalf("deterministic advance = (%#v, %v), want %#v", again, err, first)
	}
	changedRequest, err := Advance(nil, mustRequest(t, "responses", "other"), response)
	if err != nil || changedRequest == first {
		t.Fatalf("changed request = (%#v, %v), want distinct", changedRequest, err)
	}
	changedResponse, err := Advance(nil, request, mustResponse(t, "responses", "other"))
	if err != nil || changedResponse == first {
		t.Fatalf("changed response = (%#v, %v), want distinct", changedResponse, err)
	}
	child, err := Advance(&first, request, response)
	if err != nil || child == first {
		t.Fatalf("child = (%#v, %v), want distinct from genesis", child, err)
	}
	if first.Scheme() != "responses" || request.Scheme() != "responses" || response.Scheme() != "responses" {
		t.Fatal("scheme accessors did not preserve the codec scheme")
	}
}

func TestHistoryFingerprintRejectsInvalidConstruction(t *testing.T) {
	if _, err := FingerprintRequest("", nil); err == nil {
		t.Fatal("empty request scheme was accepted")
	}
	if _, err := FingerprintResponse("", nil); err == nil {
		t.Fatal("empty response scheme was accepted")
	}
	request := mustRequest(t, "responses", "request")
	response := mustResponse(t, "messages", "response")
	if _, err := Advance(nil, request, response); err == nil {
		t.Fatal("mismatched leaf schemes were accepted")
	}
	matchingResponse := mustResponse(t, "responses", "response")
	if _, err := Advance(&History{}, request, matchingResponse); err == nil {
		t.Fatal("zero previous history was accepted")
	}
	if _, err := Advance(nil, Request{}, matchingResponse); err == nil {
		t.Fatal("zero request was accepted")
	}
}

func TestFrameJSONValueIsSemanticAndTypePreserving(t *testing.T) {
	first, err := FrameJSONValue([]byte(` { "b": [true, null], "a": "1" } `))
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := FrameJSONValue([]byte(`{"a":"1","b":[true,null]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, reordered) {
		t.Fatal("object order or whitespace changed semantic framing")
	}
	number, err := FrameJSONValue([]byte(`1`))
	if err != nil {
		t.Fatal(err)
	}
	text, err := FrameJSONValue([]byte(`"1"`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(number, text) {
		t.Fatal("number and string received the same semantic framing")
	}
	if _, err := FrameJSONValue([]byte(`null true`)); err == nil {
		t.Fatal("multiple JSON values were accepted")
	}
}

func mustRequest(t *testing.T, scheme Scheme, material string) Request {
	t.Helper()
	value, err := FingerprintRequest(scheme, []byte(material))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustResponse(t *testing.T, scheme Scheme, material string) Response {
	t.Helper()
	value, err := FingerprintResponse(scheme, []byte(material))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
