package exchange

import (
	"context"
	"fmt"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
	"github.com/swobuforge/swobu/internal/session"
)

func TestLoadCheckpointAncestryPreservesThreeResponsesBatchesOldestFirst(t *testing.T) {
	store := session.NewMemoryStore()
	var predecessor *canonical.SwobuResponseID
	var latest session.Checkpoint
	for turn := 1; turn <= 3; turn++ {
		id := canonical.SwobuResponseID(fmt.Sprintf("resp_%d", turn))
		request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, fmt.Sprintf("input_%d", turn))}})
		response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "gpt", []canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, fmt.Sprintf("answer_%d", turn))}, "completed", canonical.NewUnknownTokenUsage())
		if err != nil {
			t.Fatal(err)
		}
		batch, err := responsesnative.NewItems([][]byte{[]byte(fmt.Sprintf(`{"type":"future_%d","turn":%d}`, turn, turn))})
		if err != nil {
			t.Fatal(err)
		}
		checkpoint := session.Checkpoint{Predecessor: predecessor, InputSegment: request, Request: request, Response: response, ResponsesOutput: batch}
		if err := store.Put(context.Background(), "dev", checkpoint); err != nil {
			t.Fatal(err)
		}
		latest = checkpoint
		value := id
		predecessor = &value
	}
	ancestry, err := loadCheckpointAncestry(context.Background(), store, "dev", latest)
	if err != nil {
		t.Fatal(err)
	}
	resolved := session.WithResponsesHistory(session.ResolvedRequest{}, ancestry)
	turns := resolved.Responses.History().Turns()
	if len(turns) != 3 {
		t.Fatalf("history turns=%d", len(turns))
	}
	for index, turn := range turns {
		want := fmt.Sprintf(`{"type":"future_%d","turn":%d}`, index+1, index+1)
		if got := string(turn.Output().JSONObjects()[0]); got != want {
			t.Fatalf("turn %d = %s, want %s", index, got, want)
		}
	}
}

func TestCheckpointLookupDoesNotLoadResponsesAncestryBeforeTargetSelection(t *testing.T) {
	base := session.NewMemoryStore()
	firstResponse, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp_first"}, "gpt", nil, "completed", canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	first := session.Checkpoint{Request: testCanonicalRequest("gpt"), Response: firstResponse}
	if err := base.Put(context.Background(), "dev", first); err != nil {
		t.Fatal(err)
	}
	firstID := canonical.SwobuResponseID("resp_first")
	latestResponse, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp_latest"}, "gpt", nil, "completed", canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	latest := session.Checkpoint{Predecessor: &firstID, Request: testCanonicalRequest("gpt"), Response: latestResponse}
	if err := base.Put(context.Background(), "dev", latest); err != nil {
		t.Fatal(err)
	}
	store := &countingCheckpointStore{base: base}

	event := executeCommand(context.Background(), loadCheckpointCommand{
		store: store, workspaceSlug: "dev", explicit: true, reference: "resp_latest",
	})
	loaded, ok := event.(checkpointLoaded)
	if !ok || loaded.err != nil {
		t.Fatalf("checkpoint load = (%T, %v)", event, loaded.err)
	}
	if store.getCalls != 1 {
		t.Fatalf("checkpoint lookup reads = %d, want exactly the selected checkpoint", store.getCalls)
	}
}

func TestLoadResponsesAncestryTreatsMissingPredecessorAsUnavailableNativeRefinement(t *testing.T) {
	store := session.NewMemoryStore()
	missing := canonical.SwobuResponseID("resp_pruned")
	latestResponse, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp_latest"},
		"gpt", nil, "completed", canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	latest := session.Checkpoint{
		Predecessor: &missing,
		Request:     testCanonicalRequest("gpt"),
		Response:    latestResponse,
	}
	if err := store.Put(context.Background(), "dev", latest); err != nil {
		t.Fatal(err)
	}

	event := executeCommand(context.Background(), loadResponsesAncestryCommand{
		store: store, workspaceSlug: "dev", latest: latest,
	})
	loaded, ok := event.(responsesAncestryLoaded)
	if !ok {
		t.Fatalf("event = %T, want responsesAncestryLoaded", event)
	}
	if loaded.history.Len() != 0 {
		t.Fatalf("native Responses history = %d, want unavailable refinement", loaded.history.Len())
	}
}

func TestLoadCheckpointAncestryRejectsCycleAsCheckpointCorruption(t *testing.T) {
	store := session.NewMemoryStore()
	firstID := canonical.SwobuResponseID("resp_first")
	secondID := canonical.SwobuResponseID("resp_second")
	firstResponse, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: firstID}, "gpt", nil, "completed", canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	secondResponse, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: secondID}, "gpt", nil, "completed", canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	first := session.Checkpoint{Predecessor: &secondID, Request: testCanonicalRequest("gpt"), Response: firstResponse}
	second := session.Checkpoint{Predecessor: &firstID, Request: testCanonicalRequest("gpt"), Response: secondResponse}
	for _, checkpoint := range []session.Checkpoint{first, second} {
		if err := store.Put(context.Background(), "dev", checkpoint); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := loadCheckpointAncestry(context.Background(), store, "dev", first); err == nil {
		t.Fatal("cyclic ancestry was treated as an optional native refinement")
	}
}
