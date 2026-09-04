package continuity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/thread"
)

func TestMemoryStoreIndexesOnlyCurrentHeadsAndAdvancesAtomically(t *testing.T) {
	store := NewMemoryStore()
	firstHistory := testHistory(t, "first")
	first := storeRecord("resp_1", &firstHistory)
	sessionValue, err := store.StartThread(context.Background(), "alpha", first)
	if err != nil {
		t.Fatal(err)
	}
	if got, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", firstHistory, thread.ID{}); err != nil || resolution != HistoryUniqueHead || got.ResponseID != "resp_1" {
		t.Fatalf("first head = (%q, %v, %v)", got.ResponseID, resolution, err)
	}
	secondHistory := testHistory(t, "second")
	second := storeRecord("resp_2", &secondHistory)
	second.ThreadID = sessionValue.ID
	if err := store.AdvanceThread(context.Background(), "alpha", sessionValue.ID, "resp_1", second); err != nil {
		t.Fatal(err)
	}
	if _, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", firstHistory, thread.ID{}); err != nil || resolution != HistoryNotFound {
		t.Fatalf("old history resolution = (%v, %v)", resolution, err)
	}
	if got, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", secondHistory, thread.ID{}); err != nil || resolution != HistoryUniqueHead || got.ResponseID != "resp_2" {
		t.Fatalf("second head = (%q, %v, %v)", got.ResponseID, resolution, err)
	}
	if err := store.AdvanceThread(context.Background(), "alpha", sessionValue.ID, "resp_1", storeRecord("resp_3", nil)); !errors.Is(err, ErrStaleThreadHead) {
		t.Fatalf("stale advance error = %v", err)
	}
	if old, found, err := store.GetCheckpoint(context.Background(), "alpha", "resp_1"); err != nil || !found || old.ResponseID != "resp_1" {
		t.Fatalf("explicit old checkpoint = (%q, %t, %v)", old.ResponseID, found, err)
	}
}

func TestMemoryStoreReportsAmbiguousCurrentHeads(t *testing.T) {
	store := NewMemoryStore()
	history := testHistory(t, "shared")
	if _, err := store.StartThread(context.Background(), "alpha", storeRecord("resp_1", &history)); err != nil {
		t.Fatal(err)
	}
	second := storeRecord("resp_2", &history)
	second.ThreadID = testThreadID("thread-2")
	if _, err := store.StartThread(context.Background(), "alpha", second); err != nil {
		t.Fatal(err)
	}
	if _, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", history, thread.ID{}); err != nil || resolution != HistoryAmbiguous {
		t.Fatalf("resolution = (%v, %v), want ambiguous", resolution, err)
	}
}

func TestMemoryStoreAmbiguousHistoryBecomesUniqueAfterOneHeadExpires(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	history := testHistory(t, "shared-expiry")
	first := storeRecord("resp_1", &history)
	firstExpiry := now.Add(time.Minute)
	first.ExpiresAt = &firstExpiry
	if _, err := store.StartThread(context.Background(), "alpha", first); err != nil {
		t.Fatal(err)
	}
	second := storeRecord("resp_2", &history)
	second.ThreadID = testThreadID("thread-2")
	secondExpiry := now.Add(2 * time.Minute)
	second.ExpiresAt = &secondExpiry
	if _, err := store.StartThread(context.Background(), "alpha", second); err != nil {
		t.Fatal(err)
	}
	if _, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", history, thread.ID{}); err != nil || resolution != HistoryAmbiguous {
		t.Fatalf("initial resolution = (%v, %v), want ambiguous", resolution, err)
	}
	now = now.Add(90 * time.Second)
	got, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", history, thread.ID{})
	if err != nil || resolution != HistoryUniqueHead || got.ResponseID != "resp_2" {
		t.Fatalf("post-expiry resolution = (%q, %v, %v), want resp_2 unique", got.ResponseID, resolution, err)
	}
}

func TestMemoryStorePartitionsWorkspacesAndExpiresHeads(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	history := testHistory(t, "shared")
	record := storeRecord("resp", &history)
	expires := now.Add(time.Minute)
	record.ExpiresAt = &expires
	if _, err := store.StartThread(context.Background(), "alpha", record); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.GetCheckpoint(context.Background(), "beta", "resp"); err != nil || found {
		t.Fatalf("cross-workspace get = (%t, %v)", found, err)
	}
	now = now.Add(2 * time.Minute)
	if _, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", history, thread.ID{}); err != nil || resolution != HistoryNotFound {
		t.Fatalf("expired resolution = (%v, %v)", resolution, err)
	}
}

func TestMemoryStoreConcurrentAdvanceAllowsExactlyOneWinner(t *testing.T) {
	store := NewMemoryStore()
	firstHistory := testHistory(t, "concurrent-first")
	started, err := store.StartThread(context.Background(), "alpha", storeRecord("resp_first", &firstHistory))
	if err != nil {
		t.Fatal(err)
	}
	secondHistory := testHistory(t, "concurrent-second")
	thirdHistory := testHistory(t, "concurrent-third")
	candidates := []Checkpoint{storeRecord("resp_second", &secondHistory), storeRecord("resp_third", &thirdHistory)}
	for index := range candidates {
		candidates[index].ThreadID = started.ID
	}
	start := make(chan struct{})
	results := make(chan error, len(candidates))
	var group sync.WaitGroup
	for _, candidate := range candidates {
		candidate := candidate
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- store.AdvanceThread(context.Background(), "alpha", started.ID, "resp_first", candidate)
		}()
	}
	close(start)
	group.Wait()
	close(results)
	winners := 0
	stale := 0
	for result := range results {
		switch {
		case result == nil:
			winners++
		case errors.Is(result, ErrStaleThreadHead):
			stale++
		default:
			t.Fatalf("concurrent advance error = %v", result)
		}
	}
	if winners != 1 || stale != 1 {
		t.Fatalf("concurrent results winners=%d stale=%d, want 1/1", winners, stale)
	}
}

func TestMemoryStoreRejectsCrossSchemeAdvanceWithoutMutation(t *testing.T) {
	store := NewMemoryStore()
	firstHistory := testHistory(t, "scheme-first")
	started, err := store.StartThread(context.Background(), "alpha", storeRecord("resp_first", &firstHistory))
	if err != nil {
		t.Fatal(err)
	}
	otherHistory := testHistoryForScheme(t, "messages/v1", "scheme-other")
	proposed := storeRecord("resp_other", &otherHistory)
	proposed.ThreadID = started.ID
	if err := store.AdvanceThread(context.Background(), "alpha", started.ID, "resp_first", proposed); !errors.Is(err, ErrThreadSchemeMismatch) {
		t.Fatalf("cross-scheme advance error = %v", err)
	}
	if current, err := store.IsCurrentHead(context.Background(), "alpha", started.ID, "resp_first"); err != nil || !current {
		t.Fatalf("original head current = (%t, %v)", current, err)
	}
	if got, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", firstHistory, thread.ID{}); err != nil || resolution != HistoryUniqueHead || got.ResponseID != "resp_first" {
		t.Fatalf("original index = (%q, %v, %v)", got.ResponseID, resolution, err)
	}
	if _, found, err := store.GetCheckpoint(context.Background(), "alpha", "resp_other"); err != nil || found {
		t.Fatalf("proposed checkpoint stored = (%t, %v)", found, err)
	}
}

func TestMemoryStoreRejectedStartsLeaveAllStateUnchanged(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	invalidID := storeRecord("resp_wrong", nil)
	invalidID.ResponseID = "resp_other"
	expired := storeRecord("resp_expired", nil)
	expires := now
	expired.ExpiresAt = &expires
	cases := []struct {
		name      string
		workspace string
		record    Checkpoint
	}{
		{name: "workspace", workspace: " ", record: storeRecord("resp_workspace", nil)},
		{name: "scheme", workspace: "alpha", record: func() Checkpoint { value := storeRecord("resp_scheme", nil); value.HistoryScheme = ""; return value }()},
		{name: "response ID", workspace: "alpha", record: invalidID},
		{name: "expired", workspace: "alpha", record: expired},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := memoryStoreShapeOf(store)
			if _, err := store.StartThread(context.Background(), test.workspace, test.record); err == nil {
				t.Fatal("invalid start succeeded")
			}
			if after := memoryStoreShapeOf(store); after != before {
				t.Fatalf("store shape changed: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestMemoryStoreRejectedAdvancesLeaveHeadAndIndexesUnchanged(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	history := testHistory(t, "advance-base")
	started, err := store.StartThread(context.Background(), "alpha", storeRecord("resp_first", &history))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := storeRecord("resp_first", &history)
	mismatch := storeRecord("resp_mismatch", nil)
	mismatch.ThreadID = testThreadID("other-thread")
	invalidID := storeRecord("resp_invalid", nil)
	invalidID.ResponseID = "resp_other"
	expired := storeRecord("resp_expired", nil)
	expires := now
	expired.ExpiresAt = &expires
	cases := []struct {
		name     string
		threadID thread.ID
		record   Checkpoint
	}{
		{name: "duplicate checkpoint", threadID: started.ID, record: duplicate},
		{name: "mismatched thread", threadID: started.ID, record: mismatch},
		{name: "invalid response ID", threadID: started.ID, record: invalidID},
		{name: "expired", threadID: started.ID, record: expired},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := memoryStoreShapeOf(store)
			if err := store.AdvanceThread(context.Background(), "alpha", test.threadID, "resp_first", test.record); err == nil {
				t.Fatal("invalid advance succeeded")
			}
			if after := memoryStoreShapeOf(store); after != before {
				t.Fatalf("store shape changed: before=%+v after=%+v", before, after)
			}
			if current, err := store.IsCurrentHead(context.Background(), "alpha", started.ID, "resp_first"); err != nil || !current {
				t.Fatalf("original head current = (%t, %v)", current, err)
			}
			if got, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", history, thread.ID{}); err != nil || resolution != HistoryUniqueHead || got.ResponseID != "resp_first" {
				t.Fatalf("original index = (%q, %v, %v)", got.ResponseID, resolution, err)
			}
		})
	}
}

func TestMemoryStoreReturnsDefensiveCopies(t *testing.T) {
	store := NewMemoryStore()
	history := testHistory(t, "clone")
	record := storeRecord("resp_clone", &history)
	continuation := &canonical.ResponsesContinuation{ProviderResponseID: "provider_response", TargetID: "target", TargetVersion: 1}
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp_clone", Responses: continuation}, "test-model", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	record.Response = response
	started, err := store.StartThread(context.Background(), "alpha", record)
	if err != nil {
		t.Fatal(err)
	}
	started.Head = "mutated"
	started.Scheme = "messages/v1"
	loaded, found, err := store.GetCheckpoint(context.Background(), "alpha", "resp_clone")
	if err != nil || !found {
		t.Fatalf("get = (%t, %v)", found, err)
	}
	ref := loaded.Response.Response()
	ref.Responses.ProviderResponseID = "mutated_provider"
	loaded.History = nil
	again, found, err := store.GetCheckpoint(context.Background(), "alpha", "resp_clone")
	if err != nil || !found {
		t.Fatalf("second get = (%t, %v)", found, err)
	}
	if again.History == nil || again.Response.Response().Responses.ProviderResponseID != "provider_response" {
		t.Fatalf("stored checkpoint was mutated: %#v", again)
	}
	if current, err := store.IsCurrentHead(context.Background(), "alpha", record.ThreadID, "resp_clone"); err != nil || !current {
		t.Fatalf("stored thread was mutated: (%t, %v)", current, err)
	}
}

func TestMemoryStoreCapacityEvictionRemovesSessionAndHistoryIndex(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	oldestHistory := testHistory(t, "oldest")
	oldest := storeRecord("resp_oldest", &oldestHistory)
	expires := now.Add(time.Minute)
	oldest.ExpiresAt = &expires
	if _, err := store.StartThread(context.Background(), "alpha", oldest); err != nil {
		t.Fatal(err)
	}
	for index := 1; index < maxMemoryStoreRecords; index++ {
		record := storeRecord(canonical.SwobuResponseID(fmt.Sprintf("resp_%04d", index)), nil)
		record.ThreadID = testThreadID(string(record.ResponseID))
		expires := now.Add(time.Duration(index+1) * time.Minute)
		record.ExpiresAt = &expires
		if _, err := store.StartThread(context.Background(), "alpha", record); err != nil {
			t.Fatal(err)
		}
	}
	trigger := storeRecord("resp_trigger", nil)
	expires = now.Add(48 * time.Hour)
	trigger.ExpiresAt = &expires
	if _, err := store.StartThread(context.Background(), "alpha", trigger); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.GetCheckpoint(context.Background(), "alpha", "resp_oldest"); err != nil || found {
		t.Fatalf("evicted checkpoint = (%t, %v)", found, err)
	}
	if _, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", oldestHistory, thread.ID{}); err != nil || resolution != HistoryNotFound {
		t.Fatalf("evicted history resolution = (%v, %v)", resolution, err)
	}
	if current, err := store.IsCurrentHead(context.Background(), "alpha", oldest.ThreadID, "resp_oldest"); err != nil || current {
		t.Fatalf("evicted thread current = (%t, %v)", current, err)
	}
}

func TestMemoryStorePreferredThreadDisambiguatesEqualHistory(t *testing.T) {
	store := NewMemoryStore()
	history := testHistory(t, "shared-preferred")
	first := storeRecord("resp_1", &history)
	second := storeRecord("resp_2", &history)
	second.ThreadID = testThreadID("preferred-second")
	if _, err := store.StartThread(context.Background(), "alpha", first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartThread(context.Background(), "alpha", second); err != nil {
		t.Fatal(err)
	}
	got, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", history, second.ThreadID)
	if err != nil || resolution != HistoryUniqueHead || got.ResponseID != second.ResponseID {
		t.Fatalf("preferred resolution = (%q,%v,%v), want resp_2 unique", got.ResponseID, resolution, err)
	}
	unknown := testThreadID("unknown")
	if _, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", history, unknown); err != nil || resolution != HistoryNotFound {
		t.Fatalf("foreign preferred resolution = (%v,%v), want not found", resolution, err)
	}
}

func TestMemoryStoreSupportsConcurrentStartGetAndResolve(t *testing.T) {
	store := NewMemoryStore()
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			history := testHistory(t, fmt.Sprintf("concurrent-%d", index))
			id := canonical.SwobuResponseID(fmt.Sprintf("resp_%d", index))
			if _, err := store.StartThread(context.Background(), "alpha", storeRecord(id, &history)); err != nil {
				t.Errorf("start %d: %v", index, err)
				return
			}
			if _, found, err := store.GetCheckpoint(context.Background(), "alpha", id); err != nil || !found {
				t.Errorf("get %d = (%t, %v)", index, found, err)
			}
			if got, resolution, err := store.ResolveHeadByHistory(context.Background(), "alpha", history, thread.ID{}); err != nil || resolution != HistoryUniqueHead || got.ResponseID != id {
				t.Errorf("resolve %d = (%q, %v, %v)", index, got.ResponseID, resolution, err)
			}
		}()
	}
	group.Wait()
}

func testHistory(t *testing.T, material string) historyfingerprint.History {
	return testHistoryForScheme(t, "responses/v1", material)
}

func testHistoryForScheme(t *testing.T, scheme historyfingerprint.Scheme, material string) historyfingerprint.History {
	t.Helper()
	request, err := historyfingerprint.FingerprintRequest(scheme, []byte("request:"+material))
	if err != nil {
		t.Fatal(err)
	}
	response, err := historyfingerprint.FingerprintResponse(scheme, []byte("response:"+material))
	if err != nil {
		t.Fatal(err)
	}
	history, err := historyfingerprint.Advance(nil, request, response)
	if err != nil {
		t.Fatal(err)
	}
	return history
}

func storeRecord(id canonical.SwobuResponseID, history *historyfingerprint.History) Checkpoint {
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "test-model", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		panic(err)
	}
	scheme := historyfingerprint.Scheme("responses/v1")
	if history != nil {
		scheme = history.Scheme()
	}
	return Checkpoint{ResponseID: id, ThreadID: testThreadID(string(id)), HistoryScheme: scheme, History: history, Response: response, CreatedAt: time.Now().UTC()}
}

func testThreadID(material string) thread.ID {
	id, err := thread.Derive("continuity-test/v1", material)
	if err != nil {
		panic(err)
	}
	return id
}

type memoryStoreShape struct {
	records   int
	threads   int
	byHistory int
	expires   int
}

func memoryStoreShapeOf(store *memoryStore) memoryStoreShape {
	return memoryStoreShape{records: len(store.records), threads: len(store.threads), byHistory: len(store.byHistory), expires: store.expires.Len()}
}
