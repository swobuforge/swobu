package canonical

import "testing"

func TestCanonicalResponseRejectsRequestOnlyItems(t *testing.T) {
	user, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("user")})
	callID, _ := NewToolCallID("call_1")
	result, _ := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("result")}, false)
	for name, item := range map[string]CanonicalItem{"user message": user, "tool result": result} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCanonicalResponse(ResponseRef{SwobuID: "resp_1"}, "m", []CanonicalItem{item}, "stop", NewUnknownTokenUsage()); err == nil {
				t.Fatal("request-only item was accepted as provider output")
			}
		})
	}
}

func TestCanonicalResponseRequiresCompleteSuccessIdentity(t *testing.T) {
	validRef := ResponseRef{SwobuID: "resp_1"}
	for name, response := range map[string]struct {
		ref    ResponseRef
		model  string
		finish string
	}{
		"response identity": {model: "m", finish: "stop"},
		"model":             {ref: validRef, finish: "stop"},
		"finish reason":     {ref: validRef, model: "m"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCanonicalResponse(response.ref, response.model, nil, response.finish, NewUnknownTokenUsage()); err == nil {
				t.Fatalf("missing %s was accepted", name)
			}
		})
	}
}
