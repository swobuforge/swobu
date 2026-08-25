package exchange

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

func bindReasoningItemForTest(item canonical.CanonicalItem, targetID string, targetVersion uint64) canonical.CanonicalItem {
	stream := canonical.NewSliceEventReader([]canonical.Event{
		{
			Kind: canonical.EventResponseIdentity,
			Payload: canonical.ResponseIdentityPayload{
				Response: canonical.ResponseRef{SwobuID: "resp_test"},
			},
		},
		{
			Kind: canonical.EventItemCompleted,
			Payload: canonical.ItemEvent{
				Position: canonical.ItemPosition{Item: 0},
				Payload: canonical.ItemCompletedPayload{
					Item: item,
				},
			},
		},
	})
	boundStream := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{
		SwobuID:       "resp_test",
		TargetID:      targetID,
		TargetVersion: targetVersion,
	})
	boundStream.Next(nil)          // consume identity
	ev, _ := boundStream.Next(nil) // consume completed item
	return ev.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
}

func TestProjectOpaqueReplayForTargetPreservesExactTargetReplay(t *testing.T) {
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "exact-target-cipher",
		ItemID:           "rs_exact",
	})
	if err != nil {
		t.Fatal(err)
	}
	summaryPart, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "alpha thoughts")
	unboundReasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{summaryPart}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem := bindReasoningItemForTest(unboundReasoning, "target-alpha", 1)
	msgItem, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("hello")})
	if err != nil {
		t.Fatal(err)
	}

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model-a"),
		Items: []canonical.CanonicalItem{msgItem, reasoningItem},
	})

	// Project against exact target generation: target-alpha:1
	projected, changes, err := projectOpaqueReplayForTarget(req, "target-alpha", 1)
	if err != nil {
		t.Fatalf("projection error: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes, got %d", len(changes))
	}
	if len(projected.Items()) != 2 {
		t.Fatalf("items count = %d, want 2", len(projected.Items()))
	}
	reasoning, ok := projected.Items()[1].Reasoning()
	if !ok {
		t.Fatal("item 1 is not reasoning")
	}
	replay, ok := reasoning.Opaque().Responses()
	if !ok || replay.EncryptedContent != "exact-target-cipher" || replay.ItemID != "rs_exact" {
		t.Fatalf("opaque replay not preserved: (%+v, %t)", replay, ok)
	}
}

func TestProjectOpaqueReplayForTargetStripsMismatchedReplayAndPreservesReadableReasoning(t *testing.T) {
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "alpha-cipher",
		ItemID:           "rs_alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	summaryPart, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "alpha thoughts")
	tracePart, _ := canonical.NewReasoningPart(canonical.ReasoningPartTrace, "alpha trace")
	unboundReasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{summaryPart, tracePart}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem := bindReasoningItemForTest(unboundReasoning, "target-alpha", 1)
	userMsg, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("hello")})
	if err != nil {
		t.Fatal(err)
	}
	assistantMsg, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("response text")})
	if err != nil {
		t.Fatal(err)
	}

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model-b"),
		Items: []canonical.CanonicalItem{userMsg, reasoningItem, assistantMsg},
	})

	// 1. Project against different target ID: target-beta:1
	projectedBeta, changesBeta, err := projectOpaqueReplayForTarget(req, "target-beta", 1)
	if err != nil {
		t.Fatalf("projection error: %v", err)
	}
	if len(changesBeta) != 1 {
		t.Fatalf("expected 1 omission change, got %d: %#v", len(changesBeta), changesBeta)
	}
	if changesBeta[0].Kind != compat.Omission || changesBeta[0].Capability != canonical.RequestItemsReasoningReplay || changesBeta[0].Occurrence.Key() != "request:1" {
		t.Fatalf("unexpected change: %#v", changesBeta[0])
	}
	if len(projectedBeta.Items()) != 3 {
		t.Fatalf("items count = %d, want 3", len(projectedBeta.Items()))
	}
	reasoningBeta, ok := projectedBeta.Items()[1].Reasoning()
	if !ok {
		t.Fatal("item 1 is not reasoning")
	}
	if !reasoningBeta.Opaque().IsZero() {
		t.Fatalf("opaque was not stripped: %#v", reasoningBeta.Opaque())
	}
	parts := reasoningBeta.Parts()
	if len(parts) != 2 || parts[0].Text() != "alpha thoughts" || parts[1].Text() != "alpha trace" {
		t.Fatalf("readable reasoning parts lost: %#v", parts)
	}

	// 2. Project against different target Version: target-alpha:2
	projectedAlpha2, changesAlpha2, err := projectOpaqueReplayForTarget(req, "target-alpha", 2)
	if err != nil {
		t.Fatalf("projection error: %v", err)
	}
	if len(changesAlpha2) != 1 {
		t.Fatalf("expected 1 omission change, got %d", len(changesAlpha2))
	}
	reasoningAlpha2, _ := projectedAlpha2.Items()[1].Reasoning()
	if !reasoningAlpha2.Opaque().IsZero() {
		t.Fatal("opaque was not stripped on version mismatch")
	}
}

func TestProjectOpaqueReplayForTargetDropsOpaqueOnlyReasoningItem(t *testing.T) {
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "opaque-only-cipher",
		ItemID:           "rs_opaque_only",
	})
	if err != nil {
		t.Fatal(err)
	}
	// ReasoningItem with zero readable parts (opaque-only)
	unboundReasoning, err := canonical.NewReasoningItem(nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	opaqueOnlyReasoning := bindReasoningItemForTest(unboundReasoning, "target-alpha", 1)
	userMsg, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("hello")})
	if err != nil {
		t.Fatal(err)
	}
	assistantMsg, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("answer")})
	if err != nil {
		t.Fatal(err)
	}

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model-b"),
		Items: []canonical.CanonicalItem{userMsg, opaqueOnlyReasoning, assistantMsg},
	})

	// Project against different target: target-beta:1
	projected, changes, err := projectOpaqueReplayForTarget(req, "target-beta", 1)
	if err != nil {
		t.Fatalf("projection error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if len(projected.Items()) != 2 {
		t.Fatalf("items count = %d, want 2 (opaque-only reasoning item dropped completely)", len(projected.Items()))
	}
	if projected.Items()[0].Kind() != canonical.ItemKindMessage || projected.Items()[1].Kind() != canonical.ItemKindMessage {
		t.Fatalf("unexpected items after drop: %#v", projected.Items())
	}
}

func TestProjectOpaqueReplayForTargetDropsUnboundClientReplay(t *testing.T) {
	// Unbound opaque thinking (origin == nil, e.g. client ingress without checkpoint)
	unboundOpaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "client-supplied-cipher",
		ItemID:           "rs_client",
	})
	if err != nil {
		t.Fatal(err)
	}
	summaryPart, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "client visible thought")
	reasoningItem, err := canonical.NewReasoningItem([]canonical.ReasoningPart{summaryPart}, unboundOpaque)
	if err != nil {
		t.Fatal(err)
	}
	userMsg, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("question")})
	if err != nil {
		t.Fatal(err)
	}

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model-a"),
		Items: []canonical.CanonicalItem{userMsg, reasoningItem},
	})

	projected, changes, err := projectOpaqueReplayForTarget(req, "target-alpha", 1)
	if err != nil {
		t.Fatalf("projection error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 omission change for unbound replay, got %d", len(changes))
	}
	if changes[0].Kind != compat.Omission || changes[0].Capability != canonical.RequestItemsReasoningReplay {
		t.Fatalf("unexpected change: %#v", changes[0])
	}
	reasoning, ok := projected.Items()[1].Reasoning()
	if !ok {
		t.Fatal("item 1 is not reasoning")
	}
	if !reasoning.Opaque().IsZero() {
		t.Fatal("unbound opaque replay was not stripped")
	}
	if len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "client visible thought" {
		t.Fatalf("visible thought was lost: %#v", reasoning.Parts())
	}
}

func TestPrepareProviderCallTargetBoundReplayAndFallback(t *testing.T) {
	targetA := requestpathTarget(t, "deepinfra-a")
	targetB := requestpathTarget(t, "fallback-b")
	pathA, err := resolveProviderPath(targetA)
	if err != nil {
		t.Fatal(err)
	}

	// Create opaque thinking bound to target A's exact target generation
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "target-a-encrypted-reasoning",
		ItemID:           "rs_target_a",
	})
	if err != nil {
		t.Fatal(err)
	}
	summaryPart, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "target A reasoning summary")
	unboundReasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{summaryPart}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem := bindReasoningItemForTest(unboundReasoning, pathA.target.TargetID, pathA.target.TargetVersion)
	userMsg, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("hello")})
	if err != nil {
		t.Fatal(err)
	}

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(targetA.Model().String()),
		Items: []canonical.CanonicalItem{userMsg, reasoningItem},
	})

	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{targetA, targetB}}
	prepared := mustBeginSession(t, req)
	s.prepared = &prepared
	runner := reducerRuntime()

	// 1. Prepare Candidate 0 (Target A): must preserve target A bound opaque replay
	callA, _, changesA, _, err := prepareProviderCall(context.Background(), s, providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred}, runner)
	if err != nil {
		t.Fatalf("prepareProviderCall for Target A failed: %v", err)
	}
	hasReplayOmissionA := false
	for _, c := range changesA {
		if c.Capability == canonical.RequestItemsReasoningReplay {
			hasReplayOmissionA = true
		}
	}
	if hasReplayOmissionA {
		t.Fatalf("Target A should not have reasoning replay omission changes: %#v", changesA)
	}
	reasoningA, ok := callA.request.Canonical.Items()[1].Reasoning()
	if !ok || reasoningA.Opaque().IsZero() {
		t.Fatalf("Target A request lost opaque replay: %#v", callA.request.Canonical.Items())
	}

	// 2. Prepare Candidate 1 (Target B fallback): must strip Target A opaque replay and emit omission
	callB, _, changesB, _, err := prepareProviderCall(context.Background(), s, providerCallSelection{candidateIndex: 1, requestChoice: providerRequestPreferred}, runner)
	if err != nil {
		t.Fatalf("prepareProviderCall for Target B failed: %v", err)
	}
	hasReplayOmissionB := false
	for _, c := range changesB {
		if c.Capability == canonical.RequestItemsReasoningReplay && c.Kind == compat.Omission {
			hasReplayOmissionB = true
		}
	}
	if !hasReplayOmissionB {
		t.Fatalf("Target B fallback missing reasoning replay omission change: %#v", changesB)
	}
	reasoningB, ok := callB.request.Canonical.Items()[1].Reasoning()
	if !ok {
		t.Fatal("Target B request missing reasoning item")
	}
	if !reasoningB.Opaque().IsZero() {
		t.Fatalf("Target B fallback unexpectedly retained Target A opaque replay: %#v", reasoningB.Opaque())
	}
	if len(reasoningB.Parts()) != 1 || reasoningB.Parts()[0].Text() != "target A reasoning summary" {
		t.Fatalf("Target B fallback lost readable reasoning parts: %#v", reasoningB.Parts())
	}
}

func TestCheckpointRecoveryPreservesAndProjectsTargetBoundReplay(t *testing.T) {
	targetA := requestpathTarget(t, "deepinfra-a")
	targetB := requestpathTarget(t, "fallback-b")
	pathA, err := resolveProviderPath(targetA)
	if err != nil {
		t.Fatal(err)
	}

	// Turn 1 checkpoint: contains Target-A bound reasoning item in previous request/response history
	unboundOpaque, _ := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "turn1-alpha-cipher",
		ItemID:           "rs_turn1",
	})
	summaryPart, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "turn 1 reasoning")
	turn1ReasoningItem, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{summaryPart}, unboundOpaque)
	turn1Reasoning := bindReasoningItemForTest(turn1ReasoningItem, pathA.target.TargetID, pathA.target.TargetVersion)
	turn1User, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("turn 1 question")})
	turn1Assistant, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("turn 1 answer")})

	turn1Request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(targetA.Model().String()),
		Items: []canonical.CanonicalItem{turn1User},
	})
	turn1Response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{
			SwobuID: "swobu_turn1",
			Responses: &canonical.ResponsesContinuation{
				ProviderResponseID: "provider_turn1",
				TargetID:           pathA.target.TargetID,
				TargetVersion:      pathA.target.TargetVersion,
			},
		},
		pathA.target.Model,
		[]canonical.CanonicalItem{turn1Reasoning, turn1Assistant},
		canonical.Completed("stop"),
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Turn 2 incoming request referencing turn 1 checkpoint
	turn2User, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("turn 2 question")})
	turn2Request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify(targetA.Model().String()),
		Items:            []canonical.CanonicalItem{turn2User},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_turn1"},
	})

	// Resume session with checkpoint
	resolved, err := session.Resume(turn2Request, session.Checkpoint{
		ID:       "swobu_turn1",
		Request:  turn1Request,
		Response: turn1Response,
	})
	if err != nil {
		t.Fatalf("session.Resume failed: %v", err)
	}

	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{targetA, targetB}}
	s.prepared = &resolved
	runner := reducerRuntime()

	// 1. Candidate 0 (Target A): authoritative replay preserved
	callA, _, changesA, _, err := prepareProviderCall(context.Background(), s, providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred}, runner)
	if err != nil {
		t.Fatalf("prepareProviderCall Target A failed: %v", err)
	}
	hasReplayOmissionA := false
	for _, c := range changesA {
		if c.Capability == canonical.RequestItemsReasoningReplay {
			hasReplayOmissionA = true
		}
	}
	if hasReplayOmissionA {
		t.Fatalf("Target A should not omit reasoning replay: %#v", changesA)
	}
	// Verify that the reasoning item in fullRequest has Target A origin
	itemsA := callA.request.Canonical.Items()
	foundReasoningA := false
	for _, it := range itemsA {
		if it.Kind() == canonical.ItemKindReasoning {
			foundReasoningA = true
			r, _ := it.Reasoning()
			if !r.Opaque().MatchesTarget(pathA.target.TargetID, pathA.target.TargetVersion) {
				t.Fatalf("Target A reasoning replay target mismatch: %#v", r.Opaque())
			}
		}
	}
	if !foundReasoningA {
		t.Fatal("Target A request did not contain reasoning item")
	}

	// 2. Candidate 1 (Target B fallback): replay omitted, readable reasoning kept
	callB, _, changesB, _, err := prepareProviderCall(context.Background(), s, providerCallSelection{candidateIndex: 1, requestChoice: providerRequestPreferred}, runner)
	if err != nil {
		t.Fatalf("prepareProviderCall Target B failed: %v", err)
	}
	hasReplayOmissionB := false
	for _, c := range changesB {
		if c.Capability == canonical.RequestItemsReasoningReplay && c.Kind == compat.Omission {
			hasReplayOmissionB = true
		}
	}
	if !hasReplayOmissionB {
		t.Fatalf("Target B should omit reasoning replay: %#v", changesB)
	}
	itemsB := callB.request.Canonical.Items()
	foundReasoningB := false
	for _, it := range itemsB {
		if it.Kind() == canonical.ItemKindReasoning {
			foundReasoningB = true
			r, _ := it.Reasoning()
			if !r.Opaque().IsZero() {
				t.Fatalf("Target B unexpectedly kept opaque replay: %#v", r.Opaque())
			}
			if len(r.Parts()) != 1 || r.Parts()[0].Text() != "turn 1 reasoning" {
				t.Fatalf("Target B lost readable reasoning parts: %#v", r.Parts())
			}
		}
	}
	if !foundReasoningB {
		t.Fatal("Target B request did not contain reasoning item")
	}
}

func TestUnknownOriginReplayDecodedFromNativeClientIngressIsStrippedBeforeDispatch(t *testing.T) {
	// Client sends a Responses request containing opaque reasoning replay (unbound origin)
	// without any server checkpoint / session resume.
	rawClientJSON := `{
		"model": "deepinfra-a",
		"input": [
			{
				"type": "reasoning",
				"id": "rs_client_origin",
				"status": "completed",
				"encrypted_content": "ey_client_unbound_secret_ciphertext",
				"summary": [{"type": "summary_text", "text": "visible client thoughts"}]
			},
			{
				"type": "message",
				"role": "user",
				"content": "hello provider"
			}
		]
	}`

	decoded, err := (responses.ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, []byte(rawClientJSON), carrier.Meta{}),
	)
	if err != nil {
		t.Fatalf("native client decode failed: %v", err)
	}

	// Verify decoded canonical request has reasoning with unbound origin
	decodedReasoning, ok := decoded.Request.Request.Items()[0].Reasoning()
	if !ok {
		t.Fatal("decoded item 0 is not reasoning")
	}
	if decodedReasoning.Opaque().MatchesTarget("deepinfra-a", 1) {
		t.Fatal("decoded reasoning unexpectedly matched target before binding")
	}

	targetA := requestpathTarget(t, "deepinfra-a")
	s := reducerTestState(t)
	s.route = routePlan{targets: []routing.Target{targetA}}
	prepared := mustBeginSession(t, decoded.Request.Request)
	s.prepared = &prepared
	runner := reducerRuntime()

	// Prepare provider call on first attempt: must strip unbound opaque replay and emit omission
	call, _, changes, _, err := prepareProviderCall(context.Background(), s, providerCallSelection{candidateIndex: 0, requestChoice: providerRequestPreferred}, runner)
	if err != nil {
		t.Fatalf("prepareProviderCall failed: %v", err)
	}

	hasReplayOmission := false
	for _, c := range changes {
		if c.Capability == canonical.RequestItemsReasoningReplay && c.Kind == compat.Omission {
			hasReplayOmission = true
		}
	}
	if !hasReplayOmission {
		t.Fatalf("unbound client replay did not emit RequestItemsReasoningReplay omission: %#v", changes)
	}

	// Verify canonical request in call has stripped the opaque replay but kept readable parts
	reasoningItem, ok := call.request.Canonical.Items()[0].Reasoning()
	if !ok {
		t.Fatal("call canonical item 0 is not reasoning")
	}
	if !reasoningItem.Opaque().IsZero() {
		t.Fatalf("call canonical item 0 still has opaque replay: %#v", reasoningItem.Opaque())
	}
	if len(reasoningItem.Parts()) != 1 || reasoningItem.Parts()[0].Text() != "visible client thoughts" {
		t.Fatalf("readable reasoning part was lost: %#v", reasoningItem.Parts())
	}

	// Verify outbound provider document does NOT contain the client ciphertext
	rawProviderDoc := call.document.RawBytes()
	if bytes.Contains(rawProviderDoc, []byte("ey_client_unbound_secret_ciphertext")) {
		t.Fatalf("outbound provider document leaked unbound client ciphertext: %s", rawProviderDoc)
	}
}

type staticWorkspaceLookup struct {
	workspace routing.Workspace
}

func (l staticWorkspaceLookup) GetWorkspace(_ context.Context, _ routing.WorkspaceSlug) (routing.Workspace, error) {
	return l.workspace, nil
}

type responsesProtocolBackendCodec struct{}

func (responsesProtocolBackendCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	input := wire.ProviderEncodeInput{
		Request:         req.Canonical,
		PreviousHistory: req.PreviousHistory,
		ToolNames:       req.ToolNames,
	}
	result, err := (responses.ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(input, req.Delivery, req.ExchangeID)
	return result.Document, result.Changes, err
}

func (responsesProtocolBackendCodec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	switch in := ingress.(type) {
	case provider.DocumentIngress:
		result, err := (responses.ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, request.Canonical, request.ToolNames, in.Document, request.ExchangeID)
		return provider.DecodedResponse{Stream: result.Stream, Changes: result.Changes, ProgressiveChanges: result.ProgressiveChanges}, err
	default:
		return provider.DecodedResponse{}, canonical.InternalError("test provider ingress is unsupported")
	}
}

type responsesBackendResolver struct {
	RuntimeResolver
	transport func(context.Context, provider.TargetSnapshot, carrier.Document) (provider.Ingress, error)
}

func (responsesBackendResolver) ResolveTargetSupport(provider.TargetSnapshot) provider.TargetSupport {
	return provider.TargetSupport{}
}

func (r responsesBackendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return provider.Backend{
		Target: target,
		Codec:  responsesProtocolBackendCodec{},
		Transport: provider.BindTransport(target, func(ctx context.Context, selected provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
			return r.transport(ctx, selected, document)
		}),
	}, nil
}

func TestImplicitHistoryFingerprintResumeRestoresTargetBoundOpaqueReplay(t *testing.T) {
	// Setup workspace with route "a" -> "target-a" (Responses protocol, model "upstream-target-a")
	workspace := requestpathWorkspace(t)
	checkpointStore := session.NewMemoryStore()

	var turn1CapturedProviderReq carrier.Document
	var turn2CapturedProviderReq carrier.Document
	turn := 1

	runtime := responsesBackendResolver{
		RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
		transport: func(_ context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
			if turn == 1 {
				turn1CapturedProviderReq = doc
				return provider.DocumentIngress{
					Document: carrier.NewDocument(
						protocolkind.Responses,
						"application/json",
						nil,
						[]byte(`{
							"id": "resp_upstream_turn1",
							"model": "upstream-target-a",
							"output": [
								{
									"type": "reasoning",
									"id": "rs_REAL_PROVIDER_ORIGIN_ID",
									"status": "completed",
									"encrypted_content": "ey_authoritative_upstream_cipher_token_123",
									"summary": [{"type": "summary_text", "text": "turn 1 thought summary"}]
								},
								{
									"type": "message",
									"status": "completed",
									"role": "assistant",
									"content": [{"type": "output_text", "text": "turn 1 final answer"}]
								}
							]
						}`),
						carrier.Meta{},
					),
				}, nil
			}
			turn2CapturedProviderReq = doc
			return provider.DocumentIngress{
				Document: carrier.NewDocument(
					protocolkind.Responses,
					"application/json",
					nil,
					[]byte(`{
						"id": "resp_upstream_turn2",
						"model": "upstream-target-a",
						"output": [
							{
								"type": "message",
								"status": "completed",
								"role": "assistant",
								"content": [{"type": "output_text", "text": "turn 2 final answer"}]
							}
						]
					}`),
					carrier.Meta{},
				),
			}, nil
		},
	}

	ingress := NewIngress(
		staticWorkspaceLookup{workspace: workspace},
		runtime,
		RuntimePoliciesSpec{
			CheckpointStore: checkpointStore,
			ResponseIDs:     deterministicResponseIDGenerator{},
		},
	)

	// --- Turn 1: Client sends initial user prompt ---
	turn1ClientJSON := `{
		"model": "a",
		"input": [
			{
				"type": "message",
				"role": "user",
				"content": [{"type": "input_text", "text": "turn 1 user prompt"}]
			}
		]
	}`
	turn1Input := RequestInput{
		Workspace:       workspace.Slug(),
		Request:         NewTransportRequest("POST", "/v1/responses", nil, []byte(turn1ClientJSON)),
		ClientHandler:   "codex-tui/0.147.0",
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingNone,
		ExchangeID:      "turn1_exchange",
	}

	turn1Out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, turn1Input)
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}
	if turn1Out.Response == nil {
		t.Fatal("Turn 1 response is nil")
	}
	if turn1CapturedProviderReq.IsEmpty() {
		t.Fatal("Turn 1 provider transport was not called")
	}

	// Read client response body (as HTTP transport does), triggering stream consumption and checkpoint commit
	bufferedResp, ok := turn1Out.Response.(BufferedResponse)
	if !ok {
		t.Fatalf("expected BufferedResponse, got %T", turn1Out.Response)
	}
	turn1ClientBodyBytes, err := io.ReadAll(bufferedResp.Response.Body)
	if err != nil {
		t.Fatalf("reading Turn 1 client body failed: %v", err)
	}
	_ = bufferedResp.Response.Body.Close()

	// Verify Turn 1 client received projected presentation ID and ciphertext
	if !bytes.Contains(turn1ClientBodyBytes, []byte("ey_authoritative_upstream_cipher_token_123")) {
		t.Fatalf("Turn 1 client body missing ciphertext: %s", string(turn1ClientBodyBytes))
	}

	// --- Turn 2: Client sends full history with previous_response_id ABSENT ---
	// Deliberately mutate only the client reasoning presentation ID while keeping
	// ciphertext and summary intact to prove presentation-ID-tolerant fingerprint resolution.
	turn = 2
	turn2ClientJSON := `{
		"model": "a",
		"input": [
			{
				"type": "message",
				"role": "user",
				"content": [{"type": "input_text", "text": "turn 1 user prompt"}]
			},
			{
				"type": "reasoning",
				"id": "rs_MUTATED_PRESENTATION_ID_FROM_CLIENT",
				"status": "completed",
				"encrypted_content": "ey_authoritative_upstream_cipher_token_123",
				"summary": [{"type": "summary_text", "text": "turn 1 thought summary"}]
			},
			{
				"type": "message",
				"status": "completed",
				"role": "assistant",
				"content": [{"type": "output_text", "text": "turn 1 final answer"}]
			},
			{
				"type": "message",
				"role": "user",
				"content": [{"type": "input_text", "text": "turn 2 user prompt"}]
			}
		]
	}`

	turn2Input := RequestInput{
		Workspace:       workspace.Slug(),
		Request:         NewTransportRequest("POST", "/v1/responses", nil, []byte(turn2ClientJSON)),
		ClientHandler:   "codex-tui/0.147.0",
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingNone,
		ExchangeID:      "turn2_exchange",
	}

	turn2Out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, turn2Input)
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}
	if turn2Out.Response == nil {
		t.Fatal("Turn 2 response is nil")
	}

	// Read Turn 2 body to complete the exchange
	bufferedResp2, ok := turn2Out.Response.(BufferedResponse)
	if !ok {
		t.Fatalf("expected BufferedResponse, got %T", turn2Out.Response)
	}
	_, err = io.ReadAll(bufferedResp2.Response.Body)
	if err != nil {
		t.Fatalf("reading Turn 2 client body failed: %v", err)
	}
	_ = bufferedResp2.Response.Body.Close()

	// Verify Turn 2 outbound document sent to provider
	if turn2CapturedProviderReq.IsEmpty() {
		t.Fatal("Turn 2 provider transport was never called")
	}
	turn2OutboundBytes := turn2CapturedProviderReq.RawBytes()

	// 1. Must contain authoritative provider reasoning ID from Turn 1 checkpoint (not client-mutated ID)
	if !bytes.Contains(turn2OutboundBytes, []byte("rs_REAL_PROVIDER_ORIGIN_ID")) {
		t.Fatalf("Turn 2 outbound request missing authoritative reasoning ID rs_REAL_PROVIDER_ORIGIN_ID: %s", string(turn2OutboundBytes))
	}
	// 2. Must NOT contain the client's mutated presentation ID
	if bytes.Contains(turn2OutboundBytes, []byte("rs_MUTATED_PRESENTATION_ID_FROM_CLIENT")) {
		t.Fatalf("Turn 2 outbound request leaked client mutated presentation ID: %s", string(turn2OutboundBytes))
	}
	// 3. Must contain the ciphertext
	if !bytes.Contains(turn2OutboundBytes, []byte("ey_authoritative_upstream_cipher_token_123")) {
		t.Fatalf("Turn 2 outbound request lost ciphertext: %s", string(turn2OutboundBytes))
	}
	// 4. Must contain all messages across turns
	if !bytes.Contains(turn2OutboundBytes, []byte("turn 1 user prompt")) ||
		!bytes.Contains(turn2OutboundBytes, []byte("turn 1 final answer")) ||
		!bytes.Contains(turn2OutboundBytes, []byte("turn 2 user prompt")) {
		t.Fatalf("Turn 2 outbound request missing conversational turns: %s", string(turn2OutboundBytes))
	}
}
