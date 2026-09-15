package continuity

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

func TestMemoryStoreConcurrentSiblingCommitsBothRemainAddressable(t *testing.T) {
	store := NewMemoryStore()
	base := storeRecord("resp_base", historyFor(t, "base"))
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	candidates := []Checkpoint{storeRecord("resp_a", historyFor(t, "a")), storeRecord("resp_b", historyFor(t, "b"))}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, candidate := range candidates {
		candidate := candidate
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- store.Put(context.Background(), "alpha", Commit{Checkpoint: candidate})
		}()
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("sibling commit: %v", err)
		}
	}
	for _, id := range []canonical.SwobuResponseID{"resp_base", "resp_a", "resp_b"} {
		if _, found, err := store.Get(context.Background(), "alpha", id); err != nil || !found {
			t.Fatalf("get %s = (%t,%v)", id, found, err)
		}
	}
}

func TestMemoryStoreHistoryLookupIsUniqueOnlyAndIgnoresAffinity(t *testing.T) {
	store := NewMemoryStore()
	shared := historyFor(t, "shared")
	first := storeRecord("resp_1", shared)
	second := storeRecord("resp_2", shared)
	second.ExecutionAffinity = affinity("different")
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: first}); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.FindByHistory(context.Background(), "alpha", *shared)
	if err != nil || !found || got.Response.Response().SwobuID != "resp_1" {
		t.Fatalf("unique = (%q,%t,%v)", got.Response.Response().SwobuID, found, err)
	}
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: second}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.FindByHistory(context.Background(), "alpha", *shared); err != nil || found {
		t.Fatalf("ambiguous lookup = (%t,%v)", found, err)
	}
}

func TestMemoryStoreMaterializesLogicalRequestAndSharesPrefix(t *testing.T) {
	store := newMemoryStore()
	one := requestWithText(t, "one")
	first := storeRecord("resp_1", historyFor(t, "one"))
	first.Request = one
	first.Response = responseWithText(t, "resp_1", "assistant one")
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: first}); err != nil {
		t.Fatal(err)
	}
	full := one.WithItems(append(one.Items(), first.Response.Items()...))
	full = full.WithItems(append(full.Items(), message(t, "two")))
	second := storeRecord("resp_2", historyFor(t, "two"))
	second.Request = full
	loadedFirst, _, _ := store.Get(context.Background(), "alpha", "resp_1")
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: second, Reuse: ReuseCheckpoint(loadedFirst)}); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := store.Get(context.Background(), "alpha", "resp_2")
	if err != nil || !found || !reflect.DeepEqual(loaded.Request, full) {
		t.Fatalf("materialized request differs: found=%t err=%v", found, err)
	}
	a := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_1"}]
	b := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_2"}]
	if b.requestTail.parent != a.afterTail {
		t.Fatal("second checkpoint did not share first after-response prefix")
	}
	if got := len(a.requestTail.items) + len(a.afterTail.items) + len(b.requestTail.items); got != len(one.Items())+len(first.Response.Items())+1 {
		t.Fatalf("stored item count=%d is not linear", got)
	}
}

func TestCheckpointStorageReuseAcceptsEmptyPhysicalHistory(t *testing.T) {
	store := newMemoryStore()
	base := storeRecord("resp_empty", historyFor(t, "empty"))
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	resolved, found, err := store.Get(context.Background(), "alpha", "resp_empty")
	if err != nil || !found {
		t.Fatalf("resolve empty checkpoint=(%t,%v)", found, err)
	}
	child := storeRecord("resp_child", historyFor(t, "empty-child"))
	child.Request = requestWithText(t, "child")
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: child, Reuse: ReuseCheckpoint(resolved)}); err != nil {
		t.Fatalf("exact resume from empty physical history: %v", err)
	}
	if _, found, err := store.Get(context.Background(), "alpha", "resp_child"); err != nil || !found {
		t.Fatalf("child checkpoint=(%t,%v)", found, err)
	}
}

func TestMemoryStoreChangingPreludeRetainsHistoryLinearly(t *testing.T) {
	store := newMemoryStore()
	var previous *Checkpoint
	var historyItems []canonical.CanonicalItem
	for turn := 0; turn < 20; turn++ {
		prelude := requestDirective(t, fmt.Sprintf("instructions-%d", turn))
		current := message(t, fmt.Sprintf("user-%d", turn))
		request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: append([]canonical.CanonicalItem{prelude}, append(historyItems, current)...)})
		checkpoint := storeRecord(canonical.SwobuResponseID(fmt.Sprintf("resp_%d", turn)), historyFor(t, fmt.Sprintf("history-%d", turn)))
		checkpoint.Request = request
		checkpoint.Response = responseWithText(t, checkpoint.Response.Response().SwobuID, fmt.Sprintf("assistant-%d", turn))
		commit := Commit{Checkpoint: checkpoint}
		if previous != nil {
			commit.Reuse = ReuseCheckpoint(*previous)
		}
		if err := store.Put(context.Background(), "alpha", commit); err != nil {
			t.Fatal(err)
		}
		loaded, found, err := store.Get(context.Background(), "alpha", checkpoint.Response.Response().SwobuID)
		if err != nil || !found {
			t.Fatalf("turn %d get=(%t,%v)", turn, found, err)
		}
		previous = &loaded
		historyItems = append(historyItems, current, checkpoint.Response.Items()[0])
	}
	if got, want := store.retainedItemCount(), len(historyItems); got != want {
		t.Fatalf("stored retained-history items=%d, want %d despite changing prelude", got, want)
	}
}

func TestMemoryStoreBranchesShareHistoryDespiteDifferentPreludes(t *testing.T) {
	store := newMemoryStore()
	base := storeRecord("resp_base", historyFor(t, "base"))
	base.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{requestDirective(t, "base-prelude"), message(t, "root")}})
	base.Response = responseWithText(t, "resp_base", "base answer")
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	loaded, _, _ := store.Get(context.Background(), "alpha", "resp_base")
	for index := 0; index < 2; index++ {
		child := storeRecord(canonical.SwobuResponseID(fmt.Sprintf("resp_child_%d", index)), historyFor(t, fmt.Sprintf("child-%d", index)))
		child.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{requestDirective(t, fmt.Sprintf("child-prelude-%d", index)), message(t, "root"), base.Response.Items()[0], message(t, fmt.Sprintf("child-%d", index))}})
		if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: child, Reuse: ReuseCheckpoint(loaded)}); err != nil {
			t.Fatal(err)
		}
	}
	first := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_child_0"}]
	second := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_child_1"}]
	if first.requestTail.parent == nil || first.requestTail.parent != second.requestTail.parent {
		t.Fatal("branches did not share the resolved base history")
	}
}

func TestMemoryStoreToolRoundsStoreEachRetainedItemOnce(t *testing.T) {
	store := newMemoryStore()
	callID, err := canonical.NewToolCallID("call_1")
	if err != nil {
		t.Fatal(err)
	}
	key, err := canonical.NewToolKey("test", canonical.ToolKindFunction, "lookup")
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(canonical.EmptyJSONObject()))
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("result")}, false)
	if err != nil {
		t.Fatal(err)
	}
	base := storeRecord("resp_base", historyFor(t, "tool-base"))
	base.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{requestDirective(t, "tools-v1"), message(t, "start")}})
	base.Response = responseWithItems(t, "resp_base", []canonical.CanonicalItem{call})
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	loaded, _, _ := store.Get(context.Background(), "alpha", "resp_base")
	next := storeRecord("resp_next", historyFor(t, "tool-next"))
	next.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{requestDirective(t, "tools-v2"), message(t, "start"), call, result}})
	next.Response = responseWithText(t, "resp_next", "done")
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: next, Reuse: ReuseCheckpoint(loaded)}); err != nil {
		t.Fatal(err)
	}
	if got, want := store.retainedItemCount(), 4; got != want {
		t.Fatalf("stored tool-round items=%d, want %d", got, want)
	}
}

func TestMemoryStoreExpiryRemovesAddressAndHistoryMember(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	history := historyFor(t, "expiry")
	record := storeRecord("resp", history)
	expires := now.Add(time.Minute)
	record.ExpiresAt = &expires
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, found, err := store.Get(context.Background(), "alpha", "resp"); err != nil || found {
		t.Fatalf("expired get=(%t,%v)", found, err)
	}
	if _, found, err := store.FindByHistory(context.Background(), "alpha", *history); err != nil || found {
		t.Fatalf("expired lookup=(%t,%v)", found, err)
	}
}

func TestResolvedCheckpointTailSurvivesAddressExpiry(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	base := storeRecord("resp_base", historyFor(t, "base-expiry"))
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_base", "answer")
	expires := now.Add(time.Minute)
	base.ExpiresAt = &expires
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	resolved, _, _ := store.Get(context.Background(), "alpha", "resp_base")
	now = now.Add(2 * time.Minute)
	_, _, _ = store.Get(context.Background(), "alpha", "resp_base")
	child := storeRecord("resp_child", historyFor(t, "child-expiry"))
	child.Request = resolved.Request.WithItems(append(append(resolved.Request.Items(), resolved.Response.Items()...), message(t, "child")))
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: child, Reuse: ReuseCheckpoint(resolved)}); err != nil {
		t.Fatalf("commit after predecessor expiry: %v", err)
	}
}

func TestResolvedCheckpointTailSurvivesCapacityEviction(t *testing.T) {
	store := newMemoryStore()
	base := storeRecord("resp_base", historyFor(t, "base-capacity"))
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_base", "answer")
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	resolved, _, _ := store.Get(context.Background(), "alpha", "resp_base")
	for index := 1; index < maxMemoryStoreRecords; index++ {
		record := storeRecord(canonical.SwobuResponseID(fmt.Sprintf("resp_fill_%d", index)), nil)
		if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record}); err != nil {
			t.Fatal(err)
		}
	}
	child := storeRecord("resp_child", historyFor(t, "child-capacity"))
	child.Request = resolved.Request.WithItems(append(append(resolved.Request.Items(), resolved.Response.Items()...), message(t, "child")))
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: child, Reuse: ReuseCheckpoint(resolved)}); err != nil {
		t.Fatalf("capacity commit using pinned tail: %v", err)
	}
	if _, found, _ := store.Get(context.Background(), "alpha", "resp_child"); !found {
		t.Fatal("child missing after capacity commit")
	}
}

func TestCheckpointStorageReuseRejectsAnotherWorkspaceWithoutMutation(t *testing.T) {
	store := newMemoryStore()
	base := storeRecord("resp_base", historyFor(t, "workspace-base"))
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_base", "answer")
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	resolved, found, err := store.Get(context.Background(), "alpha", "resp_base")
	if err != nil || !found {
		t.Fatalf("resolve base=(%t,%v)", found, err)
	}
	child := storeRecord("resp_child", historyFor(t, "workspace-child"))
	child.Request = resolved.Request.WithItems(append(append(resolved.Request.Items(), resolved.Response.Items()...), message(t, "child")))
	beforeRecords := len(store.records)
	beforeItems := store.retainedItemCount()
	if err := store.Put(context.Background(), "beta", Commit{Checkpoint: child, Reuse: ReuseCheckpoint(resolved)}); err == nil {
		t.Fatal("cross-workspace storage reuse succeeded")
	}
	if len(store.records) != beforeRecords || store.retainedItemCount() != beforeItems {
		t.Fatal("rejected cross-workspace reuse mutated storage")
	}
	if _, found, err := store.Get(context.Background(), "beta", "resp_child"); err != nil || found {
		t.Fatalf("rejected child get=(%t,%v)", found, err)
	}
}

func TestCheckpointStorageReuseRejectsAnotherStoreWithoutMutation(t *testing.T) {
	source := newMemoryStore()
	base := storeRecord("resp_base", historyFor(t, "store-base"))
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_base", "answer")
	if err := source.Put(context.Background(), "alpha", Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	resolved, found, err := source.Get(context.Background(), "alpha", "resp_base")
	if err != nil || !found {
		t.Fatalf("resolve base=(%t,%v)", found, err)
	}
	target := newMemoryStore()
	existing := storeRecord("resp_existing", historyFor(t, "store-existing"))
	if err := target.Put(context.Background(), "alpha", Commit{Checkpoint: existing}); err != nil {
		t.Fatal(err)
	}
	child := storeRecord("resp_child", historyFor(t, "store-child"))
	child.Request = resolved.Request.WithItems(append(append(resolved.Request.Items(), resolved.Response.Items()...), message(t, "child")))
	beforeRecords := len(target.records)
	beforeItems := target.retainedItemCount()
	if err := target.Put(context.Background(), "alpha", Commit{Checkpoint: child, Reuse: ReuseCheckpoint(resolved)}); err == nil {
		t.Fatal("cross-store storage reuse succeeded")
	}
	if len(target.records) != beforeRecords || target.retainedItemCount() != beforeItems {
		t.Fatal("rejected cross-store reuse mutated target storage")
	}
	if _, found, err := target.Get(context.Background(), "alpha", "resp_existing"); err != nil || !found {
		t.Fatalf("existing target checkpoint=(%t,%v)", found, err)
	}
}

func TestFullStoreRejectedPutDoesNotEvict(t *testing.T) {
	store := newMemoryStore()
	for index := 0; index < maxMemoryStoreRecords; index++ {
		record := storeRecord(canonical.SwobuResponseID(fmt.Sprintf("resp_%d", index)), nil)
		if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record}); err != nil {
			t.Fatal(err)
		}
	}
	invalid := storeRecord("resp_invalid", nil)
	invalid.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, "history"), requestDirective(t, "late prelude")}})
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: invalid}); err == nil {
		t.Fatal("invalid put succeeded")
	}
	if len(store.records) != maxMemoryStoreRecords {
		t.Fatalf("records=%d after rejected put", len(store.records))
	}
	if _, found, _ := store.Get(context.Background(), "alpha", "resp_0"); !found {
		t.Fatal("rejected put evicted existing checkpoint")
	}
}

func TestAmbiguousHistoryStorageUsesOnlyClientSuppliedRepresentation(t *testing.T) {
	store := newMemoryStore()
	history := historyFor(t, "ambiguous-private")
	for index, private := range []string{"private-x", "private-y"} {
		record := storeRecord(canonical.SwobuResponseID(fmt.Sprintf("resp_%d", index)), history)
		record.Request = requestWithText(t, private)
		if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record}); err != nil {
			t.Fatal(err)
		}
	}
	clientPrefix := []canonical.CanonicalItem{message(t, "client-visible")}
	child := storeRecord("resp_child", historyFor(t, "child-visible"))
	child.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: append(clientPrefix, message(t, "current"))})
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: child, Reuse: ReuseVisibleHistory(*history, len(clientPrefix))}); err != nil {
		t.Fatal(err)
	}
	loaded, _, _ := store.Get(context.Background(), "alpha", "resp_child")
	if !reflect.DeepEqual(loaded.Request.Items(), child.Request.Items()) {
		t.Fatal("ambiguous commit inherited checkpoint-private canonical state")
	}
}

func TestVisibleHistoryIndexIsSweptAndIncludedInAccounting(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	previous := historyFor(t, "visible-root")
	record := storeRecord("resp_child", historyFor(t, "child-root"))
	record.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, "prior"), message(t, "current")}})
	expires := now.Add(time.Minute)
	record.ExpiresAt = &expires
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record, Reuse: ReuseVisibleHistory(*previous, 1)}); err != nil {
		t.Fatal(err)
	}
	if len(store.visibleHistory) != 1 || store.retainedItemCount() != 2 {
		t.Fatalf("index=%d items=%d", len(store.visibleHistory), store.retainedItemCount())
	}
	now = now.Add(2 * time.Minute)
	_, _, _ = store.Get(context.Background(), "alpha", "resp_child")
	if len(store.visibleHistory) != 0 || store.retainedItemCount() != 0 {
		t.Fatalf("orphan index=%d items=%d", len(store.visibleHistory), store.retainedItemCount())
	}
}

func TestOrdinaryLookupDoesNotSweepVisibleHistoryWithoutExpiry(t *testing.T) {
	store := newMemoryStore()
	record := storeRecord("resp_live", historyFor(t, "live"))
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record}); err != nil {
		t.Fatal(err)
	}
	history := historyFor(t, "sentinel")
	key := workspaceHistoryKey{workspaceSlug: "alpha", history: *history}
	store.visibleHistory[key] = appendSegment(nil, []canonical.CanonicalItem{message(t, "sentinel")})
	if _, found, err := store.Get(context.Background(), "alpha", "resp_live"); err != nil || !found {
		t.Fatalf("ordinary get=(%t,%v)", found, err)
	}
	if store.visibleHistory[key] == nil {
		t.Fatal("ordinary lookup performed a full visible-history sweep")
	}
}

func TestVisibleHistoryIndexIsRestoredAfterCapacityEviction(t *testing.T) {
	store := newMemoryStore()
	history := historyFor(t, "visible-capacity")
	base := storeRecord("resp_base", historyFor(t, "base-visible-capacity"))
	base.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, "prefix"), message(t, "base-current")}})
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: base, Reuse: ReuseVisibleHistory(*history, 1)}); err != nil {
		t.Fatal(err)
	}
	key := workspaceHistoryKey{workspaceSlug: "alpha", history: *history}
	original := store.visibleHistory[key]
	for index := 1; index < maxMemoryStoreRecords; index++ {
		record := storeRecord(canonical.SwobuResponseID(fmt.Sprintf("resp_fill_%d", index)), nil)
		if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record}); err != nil {
			t.Fatal(err)
		}
	}
	child := storeRecord("resp_child", historyFor(t, "child-visible-capacity"))
	child.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, "equivalent-prefix"), message(t, "child-current")}})
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: child, Reuse: ReuseVisibleHistory(*history, 1)}); err != nil {
		t.Fatal(err)
	}
	if store.visibleHistory[key] != original {
		t.Fatal("capacity eviction did not restore the selected visible-history representative")
	}
	stored := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_child"}]
	if stored.requestTail.parent != original {
		t.Fatal("child did not retain the selected visible-history representative")
	}
}

func TestVisibleHistoryReuseAllowsDifferentCanonicalCardinality(t *testing.T) {
	store := newMemoryStore()
	history := historyFor(t, "cardinality")
	first := storeRecord("resp_first", historyFor(t, "first-cardinality"))
	first.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, "representative")}})
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: first, Reuse: ReuseVisibleHistory(*history, 1)}); err != nil {
		t.Fatal(err)
	}
	second := storeRecord("resp_second", historyFor(t, "second-cardinality"))
	second.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, "incoming-a"), message(t, "incoming-b"), message(t, "current")}})
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: second, Reuse: ReuseVisibleHistory(*history, 2)}); err != nil {
		t.Fatalf("equivalent history with different item cardinality: %v", err)
	}
	loaded, _, _ := store.Get(context.Background(), "alpha", "resp_second")
	if len(loaded.Request.Items()) != 2 {
		t.Fatalf("materialized items=%d, want stored representative plus current", len(loaded.Request.Items()))
	}
}

func storeRecord(id canonical.SwobuResponseID, history *historyfingerprint.History) Checkpoint {
	return Checkpoint{History: history, ExecutionAffinity: affinity(string(id)), Response: responseWithText(nil, id, ""), CreatedAt: time.Now().UTC()}
}
func affinity(value string) executionaffinity.Key {
	key, err := executionaffinity.Derive("continuity-test/v1", value)
	if err != nil {
		panic(err)
	}
	return key
}
func historyFor(t *testing.T, value string) *historyfingerprint.History {
	t.Helper()
	q, _ := historyfingerprint.FingerprintRequest("responses/v1", []byte("q:"+value))
	a, _ := historyfingerprint.FingerprintResponse("responses/v1", []byte("a:"+value))
	h, err := historyfingerprint.Advance(nil, q, a)
	if err != nil {
		t.Fatal(err)
	}
	return &h
}
func message(t *testing.T, text string) canonical.CanonicalItem {
	item, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func requestDirective(t *testing.T, text string) canonical.CanonicalItem {
	item, err := canonical.NewScopedMessageItem(canonical.MessageRoleDeveloper, []canonical.MessagePart{canonical.NewTextMessagePart(text)}, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func requestWithText(t *testing.T, text string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, text)}})
}
func responseWithText(t *testing.T, id canonical.SwobuResponseID, text string) canonical.CanonicalResponse {
	var items []canonical.CanonicalItem
	if text != "" {
		item, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
		if err != nil {
			if t != nil {
				t.Fatal(err)
			} else {
				panic(err)
			}
		}
		items = []canonical.CanonicalItem{item}
	}
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "m", items, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		panic(fmt.Sprint(err))
	}
	return response
}
func responseWithItems(t *testing.T, id canonical.SwobuResponseID, items []canonical.CanonicalItem) canonical.CanonicalResponse {
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "m", items, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	return response
}
