package session

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResumeHistoryKeepsCompleteFallbackAndUsesCodecRebasedInvocation(t *testing.T) {
	target := testBackendTarget(t, "m")
	previousRequest := makeRequest("m", makeItems("one"), nil)
	previousResponse := makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "answer"))
	previousResponse = responseWithRef(t, previousResponse, canonical.ResponseRef{SwobuID: "resp_previous", Responses: nativeResponses(target, "provider_previous")})
	predecessorCheckpoint := Checkpoint{Request: previousRequest, Response: previousResponse}
	full := makeRequest("m", []canonical.CanonicalItem{
		mustMessageItem(canonical.MessageRoleUser, "one"),
		mustMessageItem(canonical.MessageRoleAssistant, "answer"),
		mustMessageItem(canonical.MessageRoleUser, "two"),
	}, nil)
	rebased := makeRequest("m", []canonical.CanonicalItem{
		mustMessageItem(canonical.MessageRoleUser, "two"),
	}, nil)

	resolved, err := ResumeHistory(full, rebased, predecessorCheckpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Full.Items()) != 3 {
		t.Fatalf("full items = %d, want supplied 3", len(resolved.Full.Items()))
	}
	native := resolved.ForTarget(target)
	if len(native.Items()) != 1 {
		t.Fatalf("native delta items = %d, want 1", len(native.Items()))
	}
	if previous, ok := native.PreviousResponse(); !ok || previous.SwobuID != "resp_previous" {
		t.Fatalf("native previous response = %#v", previous)
	}
}

func TestResumeHistoryInheritsValidatedPredecessorMedia(t *testing.T) {
	image, err := canonical.NewURLImage("https://example.test/mutable.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("see"),
		canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	previousRequest := makeRequest("m", []canonical.CanonicalItem{message}, nil)
	media, err := (ResolvedMedia{}).Bind(
		canonical.RequestPartRef{Item: 0, Part: 1},
		"https://example.test/mutable.png",
		canonical.ImageMediaPNG,
		[]byte("durable image bytes"),
	)
	if err != nil {
		t.Fatal(err)
	}
	previousResponse := responseWithRef(t,
		makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "seen")),
		canonical.ResponseRef{SwobuID: "resp_media"},
	)
	checkpoint := Checkpoint{Request: previousRequest, Response: previousResponse, ResolvedMedia: media}
	full := makeRequest("m", append(previousRequest.Items(),
		mustMessageItem(canonical.MessageRoleAssistant, "seen"),
		mustMessageItem(canonical.MessageRoleUser, "again"),
	), nil)
	rebased := makeRequest("m", makeItems("again"), nil)

	resolved, err := ResumeHistory(full, rebased, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	asset, ok := resolved.ResolvedMedia.Resolve(
		canonical.RequestPartRef{Item: 0, Part: 1},
		"https://example.test/mutable.png",
	)
	if !ok || string(asset.Bytes()) != "durable image bytes" {
		t.Fatalf("inherited media = (%q, %t), want durable predecessor bytes", asset.Bytes(), ok)
	}
}

func TestResumeHistoryRestoresOpaqueThinkingHiddenFromClientProjection(t *testing.T) {
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "portable summary")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := canonical.NewOpenRouterOpaqueThinking([]byte(`[{"type":"reasoning.summary","summary":"portable summary"}]`))
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, detail)
	if err != nil {
		t.Fatal(err)
	}
	answer := mustMessageItem(canonical.MessageRoleAssistant, "answer")
	previousRequest := makeRequest("m", makeItems("one"), nil)
	previousResponse := responseWithRef(t, makeResponse(reasoning, answer), canonical.ResponseRef{SwobuID: "resp_hidden"})
	checkpoint := Checkpoint{Request: previousRequest, Response: previousResponse}
	// Standard Chat history can contain only the visible answer.
	complete := makeRequest("m", []canonical.CanonicalItem{
		mustMessageItem(canonical.MessageRoleUser, "one"), answer,
		mustMessageItem(canonical.MessageRoleUser, "again"),
	}, nil)
	rebased := makeRequest("m", makeItems("again"), nil)

	resolved, err := ResumeHistory(complete, rebased, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	items := resolved.Full.Items()
	if len(items) != 4 || items[1].Kind() != canonical.ItemKindReasoning {
		t.Fatalf("restored full history = %#v", items)
	}
	restored, _ := items[1].Reasoning()
	if _, ok := restored.Opaque().OpenRouter(); !ok {
		t.Fatal("implicit resume lost hidden OpenRouter opaque thinking")
	}
}

func TestResumeHistoryRejectsMediaThatDoesNotMatchPredecessorRequest(t *testing.T) {
	image, err := canonical.NewURLImage("https://example.test/request.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	if err != nil {
		t.Fatal(err)
	}
	request := makeRequest("m", []canonical.CanonicalItem{message}, nil)
	media, err := (ResolvedMedia{}).Bind(canonical.RequestPartRef{}, "https://example.test/other.png", canonical.ImageMediaPNG, []byte("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	response := responseWithRef(t, makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "seen")), canonical.ResponseRef{SwobuID: "resp_invalid_media"})
	_, err = ResumeHistory(request, makeRequest("m", makeItems("again"), nil), Checkpoint{Request: request, Response: response, ResolvedMedia: media})
	if err == nil || !strings.Contains(err.Error(), "invalid history checkpoint media") {
		t.Fatalf("ResumeHistory error = %v", err)
	}
}

func responseWithRef(t *testing.T, response canonical.CanonicalResponse, ref canonical.ResponseRef) canonical.CanonicalResponse {
	t.Helper()
	bound, err := canonical.NewCanonicalResponse(ref, response.Model(), response.Items(), response.CompletionReason(), response.Usage())
	if err != nil {
		t.Fatal(err)
	}
	return bound
}
