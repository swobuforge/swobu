package continuity

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMemoryStoreHistoryLookupIsUniqueOnlyAndWorkspacePartitioned(t *testing.T) {
	store := newMemoryStore(defaultMemoryStoreWeight)
	shared := historyFor(t, "shared")
	first := storeRecord("resp_1", shared)
	second := storeRecord("resp_2", shared)
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: first}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := store.FindByHistory(context.Background(), "beta", *shared); found {
		t.Fatal("history crossed workspace boundary")
	}
	if got, found, err := store.FindByHistory(context.Background(), "alpha", *shared); err != nil || !found || got.Response.Response().SwobuID != "resp_1" {
		t.Fatalf("unique lookup=(%q,%t,%v)", got.Response.Response().SwobuID, found, err)
	}
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: second}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.FindByHistory(context.Background(), "alpha", *shared); err != nil || found {
		t.Fatalf("ambiguous lookup=(%t,%v)", found, err)
	}
}

func TestMemoryStoreLinearContinuationSharesAndMaterializes(t *testing.T) {
	store := newMemoryStore(defaultMemoryStoreWeight)
	base := storeRecord("resp_a", historyFor(t, "a"))
	base.Request = requestWithText(t, "one")
	base.Response = responseWithText(t, "resp_a", "answer one")
	mustPut(t, store, "alpha", Commit{Checkpoint: base})

	child := storeRecord("resp_b", historyFor(t, "b"))
	child.Request = base.Request.WithItems(append(append(base.Request.Items(), base.Response.Items()...), message(t, "two")))
	child.Response = responseWithText(t, "resp_b", "answer two")
	mustPut(t, store, "alpha", Commit{Checkpoint: child, ResolvedFrom: "resp_a"})

	a := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_a"}]
	b := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_b"}]
	if a.chain != b.chain || b.requestTail.parent != a.afterTail {
		t.Fatal("linear continuation did not extend the predecessor chain")
	}
	loaded, found, err := store.Get(context.Background(), "alpha", "resp_b")
	if err != nil || !found || !reflect.DeepEqual(loaded.Request, child.Request) {
		t.Fatalf("materialized child differs: found=%t err=%v", found, err)
	}
}

func TestMemoryStoreBranchCopiesOnceWithoutCrossChainSegments(t *testing.T) {
	store := newMemoryStore(defaultMemoryStoreWeight)
	base := storeRecord("resp_a", historyFor(t, "a"))
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_a", "a")
	mustPut(t, store, "alpha", Commit{Checkpoint: base})

	b := childRecord(t, base, "resp_b", "b")
	mustPut(t, store, "alpha", Commit{Checkpoint: b, ResolvedFrom: "resp_a"})
	c := childRecord(t, b, "resp_c", "c")
	mustPut(t, store, "alpha", Commit{Checkpoint: c, ResolvedFrom: "resp_b"})
	x := childRecord(t, b, "resp_x", "x")
	mustPut(t, store, "alpha", Commit{Checkpoint: x, ResolvedFrom: "resp_b"})

	cStored := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_c"}]
	xStored := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_x"}]
	if cStored.chain == xStored.chain {
		t.Fatal("branch reused the non-head predecessor chain")
	}
	seen := segmentSet(cStored.afterTail)
	for segment := xStored.afterTail; segment != nil; segment = segment.parent {
		if _, shared := seen[segment]; shared {
			t.Fatal("branch shares a history segment with the original chain")
		}
	}
	for _, checkpoint := range []Checkpoint{c, x} {
		loaded, found, err := store.Get(context.Background(), "alpha", checkpoint.Response.Response().SwobuID)
		if err != nil || !found || !reflect.DeepEqual(loaded.Request, checkpoint.Request) {
			t.Fatalf("branch %s materialization failed", checkpoint.Response.Response().SwobuID)
		}
	}
}

func TestMemoryStoreConcurrentSiblingsRemainAddressable(t *testing.T) {
	store := newMemoryStore(defaultMemoryStoreWeight)
	base := storeRecord("resp_base", historyFor(t, "base"))
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_base", "answer")
	mustPut(t, store, "alpha", Commit{Checkpoint: base})
	candidates := []Checkpoint{childRecord(t, base, "resp_a", "a"), childRecord(t, base, "resp_b", "b")}
	start := make(chan struct{})
	errs := make(chan error, len(candidates))
	var group sync.WaitGroup
	for _, candidate := range candidates {
		candidate := candidate
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			errs <- store.Put(context.Background(), "alpha", Commit{Checkpoint: candidate, ResolvedFrom: "resp_base"})
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []canonical.SwobuResponseID{"resp_a", "resp_b"} {
		if _, found, err := store.Get(context.Background(), "alpha", id); err != nil || !found {
			t.Fatalf("sibling %s=(%t,%v)", id, found, err)
		}
	}
}

func TestMemoryStoreEvictionPreservesDescendantAndIndex(t *testing.T) {
	store := newMemoryStore(defaultMemoryStoreWeight)
	base := storeRecord("resp_a", historyFor(t, "a"))
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_a", "answer")
	mustPut(t, store, "alpha", Commit{Checkpoint: base})
	child := childRecord(t, base, "resp_b", "child")
	mustPut(t, store, "alpha", Commit{Checkpoint: child, ResolvedFrom: "resp_a"})
	store.maxWeight = store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_b"}].localWeight + store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_a"}].chain.historyWeight
	store.mu.Lock()
	store.enforceCapacity()
	store.mu.Unlock()
	if _, found, _ := store.Get(context.Background(), "alpha", "resp_a"); found {
		t.Fatal("oldest anchor was not evicted")
	}
	loaded, found, err := store.Get(context.Background(), "alpha", "resp_b")
	if err != nil || !found || !reflect.DeepEqual(loaded.Request, child.Request) {
		t.Fatal("evicting predecessor corrupted descendant")
	}
	if _, found, _ := store.FindByHistory(context.Background(), "alpha", *base.History); found {
		t.Fatal("evicted anchor remained in history index")
	}
}

func TestMemoryStoreEvictedPredecessorCreatesFreshRoot(t *testing.T) {
	store := newMemoryStore(1)
	base := storeRecord("resp_base", historyFor(t, "base"))
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_base", "answer")
	mustPut(t, store, "alpha", Commit{Checkpoint: base})
	filler := storeRecord("resp_filler", historyFor(t, "filler"))
	mustPut(t, store, "alpha", Commit{Checkpoint: filler})
	if _, found, _ := store.Get(context.Background(), "alpha", "resp_base"); found {
		t.Fatal("expected predecessor eviction")
	}
	child := childRecord(t, base, "resp_child", "child")
	mustPut(t, store, "alpha", Commit{Checkpoint: child, ResolvedFrom: "resp_base"})
	stored := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_child"}]
	if stored.chain.anchors != 1 || stored.requestTail == nil && len(child.Request.Items()) != 0 {
		t.Fatal("evicted predecessor did not degrade to a fresh chain")
	}
}

func TestMemoryStoreBudgetCountsPreludeAndAllowsOneOversizedCheckpoint(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "large_catalog")
	declaration := canonicaltest.MustFunctionTool(key, "large", canonicaltest.Schema(t, `{"type":"object","description":"`+strings.Repeat("x", 4096)+`"}`), canonical.Unspecified[bool]())
	largePrelude := canonicaltest.ToolDeclarations(t, declaration)
	probe := storeRecord("probe", nil)
	probe.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{largePrelude, message(t, "x")}})
	preludeWeight, err := canonicalItemsWeight([]canonical.CanonicalItem{largePrelude})
	if err != nil {
		t.Fatal(err)
	}
	responseWeight, err := canonicalItemsWeight(probe.Response.Items())
	if err != nil {
		t.Fatal(err)
	}
	weight, err := checkpointLocalWeight("alpha", probe, preludeWeight, responseWeight)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryStore(weight / 2)
	for index := 0; index < 8; index++ {
		record := probe
		record.Response = responseWithText(t, canonical.SwobuResponseID(fmt.Sprintf("resp_%d", index)), "")
		mustPut(t, store, "alpha", Commit{Checkpoint: record})
	}
	if len(store.records) != 1 || store.usedWeight <= store.maxWeight {
		t.Fatalf("oversized state records=%d used=%d max=%d", len(store.records), store.usedWeight, store.maxWeight)
	}
}

func TestMemoryStorePutDoesNotCloneLargePayloadsForAccounting(t *testing.T) {
	const payloadBytes = 8 << 20
	t.Run("prelude", func(t *testing.T) {
		small := checkpointWithPrelude(t, benchmarkToolPrelude(t, 1))
		large := checkpointWithPrelude(t, benchmarkToolPrelude(t, payloadBytes))
		assertPutAllocationDelta(t, small, large, payloadBytes, payloadBytes*15/2)
	})
	t.Run("response", func(t *testing.T) {
		small := checkpointWithOpaqueResponse(t, 1)
		large := checkpointWithOpaqueResponse(t, payloadBytes)
		assertPutAllocationDelta(t, small, large, payloadBytes, payloadBytes*5/2)
	})
}

func checkpointWithPrelude(t *testing.T, prelude canonical.CanonicalItem) Checkpoint {
	t.Helper()
	record := storeRecord("allocation_probe", nil)
	record.Request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{prelude, message(t, "history")},
	})
	return record
}

func checkpointWithOpaqueResponse(t *testing.T, size int) Checkpoint {
	t.Helper()
	opaque, err := canonical.NewMessagesOpaqueThinking(make([]byte, size))
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewReasoningItem(nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "allocation_probe"}, "m", []canonical.CanonicalItem{item}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	record := storeRecord("allocation_probe", nil)
	record.Response = response
	return record
}

func assertPutAllocationDelta(t *testing.T, small, large Checkpoint, payloadBytes, maximumBytes int) {
	t.Helper()
	smallAllocated := allocatedBytesForPut(t, small)
	largeAllocated := allocatedBytesForPut(t, large)
	if largeAllocated < smallAllocated {
		t.Fatalf("large Put allocated %d bytes, below small Put allocation %d", largeAllocated, smallAllocated)
	}
	delta := largeAllocated - smallAllocated
	if delta > uint64(maximumBytes) {
		t.Fatalf("large payload allocation delta=%d bytes, want <=%d for payload=%d", delta, maximumBytes, payloadBytes)
	}
}

func allocatedBytesForPut(t *testing.T, record Checkpoint) uint64 {
	t.Helper()
	store := newMemoryStore(defaultMemoryStoreWeight)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	return after.TotalAlloc - before.TotalAlloc
}

func TestCheckpointWeightSupportsEveryCanonicalItemKind(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	declaration := canonicaltest.MustFunctionTool(key, "lookup", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	declarations := canonicaltest.ToolDeclarations(t, declaration)
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"x"}`)))
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("result")}, false)
	discovery, _ := canonical.NewToolDiscoveryResultItem(callID, canonicaltest.ToolSet(t, declaration), canonical.DiscoveryExecutorClient)
	part, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "summary")
	opaque, _ := canonical.NewMessagesOpaqueThinking([]byte(`{"type":"thinking","text":"opaque"}`))
	reasoning, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	items := []canonical.CanonicalItem{message(t, "message"), declarations, call, result, discovery, reasoning}
	for _, item := range items {
		if weight, err := canonicalItemWeight(item); err != nil || weight <= canonicalItemFloor {
			t.Fatalf("kind=%s weight=%d err=%v", item.Kind(), weight, err)
		}
	}
	short, _ := canonicalItemWeight(message(t, "x"))
	long, _ := canonicalItemWeight(message(t, "a much longer retained message"))
	if long <= short {
		t.Fatalf("message weight is not monotonic: short=%d long=%d", short, long)
	}
}

func TestMemoryStoreDuplicateAndInvalidPutDoNotMutate(t *testing.T) {
	store := newMemoryStore(defaultMemoryStoreWeight)
	record := storeRecord("resp", historyFor(t, "one"))
	mustPut(t, store, "alpha", Commit{Checkpoint: record})
	beforeRecords, beforeWeight := len(store.records), store.usedWeight
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: record}); err != ErrCheckpointExists {
		t.Fatalf("duplicate error=%v", err)
	}
	invalid := storeRecord("invalid", nil)
	invalid.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, "history"), requestDirective(t, "late")}})
	if err := store.Put(context.Background(), "alpha", Commit{Checkpoint: invalid}); err == nil {
		t.Fatal("invalid put succeeded")
	}
	if len(store.records) != beforeRecords || store.usedWeight != beforeWeight {
		t.Fatal("rejected put mutated store")
	}
}

func TestMemoryStoreDetachedSuffixHasRightSizedBackingArray(t *testing.T) {
	store := newMemoryStore(defaultMemoryStoreWeight)
	base := storeRecord("resp_a", nil)
	base.Request = requestWithText(t, "root")
	base.Response = responseWithText(t, "resp_a", "answer")
	mustPut(t, store, "alpha", Commit{Checkpoint: base})
	items := append(append(base.Request.Items(), base.Response.Items()...), message(t, "small suffix"))
	child := storeRecord("resp_b", nil)
	child.Request = base.Request.WithItems(items)
	mustPut(t, store, "alpha", Commit{Checkpoint: child, ResolvedFrom: "resp_a"})
	segment := store.records[workspaceRecordID{workspaceSlug: "alpha", id: "resp_b"}].requestTail
	if len(segment.items) != 1 || cap(segment.items) != len(segment.items) {
		t.Fatalf("incremental suffix len=%d cap=%d", len(segment.items), cap(segment.items))
	}
}

func TestMemoryStoreLinearHistoryWeightIsNotQuadratic(t *testing.T) {
	store := newMemoryStore(defaultMemoryStoreWeight)
	var previous Checkpoint
	var history []canonical.CanonicalItem
	for turn := 0; turn < 100; turn++ {
		id := canonical.SwobuResponseID(fmt.Sprintf("resp_%d", turn))
		record := storeRecord(id, nil)
		record.Request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: append(append([]canonical.CanonicalItem(nil), history...), message(t, fmt.Sprintf("u%d", turn)))})
		record.Response = responseWithText(t, id, fmt.Sprintf("a%d", turn))
		commit := Commit{Checkpoint: record}
		if turn > 0 {
			commit.ResolvedFrom = previous.Response.Response().SwobuID
		}
		mustPut(t, store, "alpha", commit)
		history = append(history, message(t, fmt.Sprintf("u%d", turn)), record.Response.Items()[0])
		previous = record
	}
	if got, want := store.retainedItemCount(), len(history); got != want {
		t.Fatalf("retained items=%d want=%d", got, want)
	}
}

func TestMemoryStoreConcurrentReadWriteStress(t *testing.T) {
	store := newMemoryStore(4 << 20)
	ctx := context.Background()
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		worker := worker
		group.Add(1)
		go func() {
			defer group.Done()
			for index := 0; index < 1000; index++ {
				id := canonical.SwobuResponseID(fmt.Sprintf("resp_%d_%d", worker, index))
				record := storeRecord(id, historyFor(t, fmt.Sprintf("%d-%d", worker, index%20)))
				_ = store.Put(ctx, fmt.Sprintf("workspace-%d", worker%2), Commit{Checkpoint: record})
				_, _, _ = store.Get(ctx, fmt.Sprintf("workspace-%d", worker%2), id)
				if record.History != nil {
					_, _, _ = store.FindByHistory(ctx, fmt.Sprintf("workspace-%d", worker%2), *record.History)
				}
			}
		}()
	}
	group.Wait()
	if len(store.records) > 1 && store.usedWeight > store.maxWeight {
		t.Fatalf("budget invariant used=%d max=%d records=%d", store.usedWeight, store.maxWeight, len(store.records))
	}
}

func BenchmarkMemoryStoreContinuityStress(b *testing.B) {
	const laneCount = 32
	store := newMemoryStore(8 << 20)
	ctx := context.Background()
	ordinaryPrelude := benchmarkToolPrelude(b, 4<<10)
	largePrelude := benchmarkToolPrelude(b, 256<<10)
	type lane struct {
		head     Checkpoint
		previous Checkpoint
		depth    int
		prelude  canonical.CanonicalItem
		seedID   canonical.SwobuResponseID
	}
	type readProbe struct {
		workspace string
		id        canonical.SwobuResponseID
		history   historyfingerprint.History
	}
	lanes := make([]lane, laneCount)
	probes := make([]atomic.Pointer[readProbe], laneCount)
	for index := range lanes {
		lanes[index].prelude = ordinaryPrelude
		if index == 0 {
			lanes[index].prelude = largePrelude
		}
		id := canonical.SwobuResponseID(fmt.Sprintf("seed_%d", index))
		lanes[index].seedID = id
		seed := storeRecord(id, historyForBenchmark(string(id)))
		seed.Request = canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: []canonical.CanonicalItem{lanes[index].prelude, message(b, "seed")},
		})
		seed.Response = responseWithText(nil, id, "seed answer")
		if err := store.Put(ctx, fmt.Sprintf("workspace-%d", index%4), Commit{Checkpoint: seed}); err != nil {
			b.Fatal(err)
		}
		lanes[index].head = seed
		probes[index].Store(&readProbe{workspace: fmt.Sprintf("workspace-%d", index%4), id: id, history: *seed.History})
	}

	var readSequence atomic.Uint64
	stopReaders := make(chan struct{})
	var readers sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				sequence := readSequence.Add(1)
				laneIndex := int(sequence % laneCount)
				probe := probes[laneIndex].Load()
				_, _, _ = store.Get(ctx, probe.workspace, probe.id)
				_, _, _ = store.FindByHistory(ctx, probe.workspace, probe.history)
			}
		}()
	}
	defer func() {
		close(stopReaders)
		readers.Wait()
	}()

	var branches, evictions, freshRoots, evictedPredecessors int
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		laneIndex := index % laneCount
		current := &lanes[laneIndex]
		parent := current.head
		branch := index%23 == 0 && current.previous.Response.Response().SwobuID != ""
		if branch {
			parent = current.previous
		}
		id := canonical.SwobuResponseID(fmt.Sprintf("resp_%d", index))
		record := storeRecord(id, historyForBenchmark(fmt.Sprintf("history-%d", index%100)))
		padding := 32
		if index%97 == 0 {
			padding = 4 << 10
		}
		if index%997 == 0 {
			padding = 64 << 10
		}
		commit := Commit{Checkpoint: record}
		if current.depth >= 12 {
			record.Request = canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("m"),
				Items: []canonical.CanonicalItem{current.prelude, message(b, strings.Repeat("x", padding))},
			})
			current.previous = Checkpoint{}
			current.depth = 0
			freshRoots++
			store.mu.RLock()
			_, seedStillAddressable := store.records[workspaceRecordID{workspaceSlug: fmt.Sprintf("workspace-%d", laneIndex%4), id: current.seedID}]
			store.mu.RUnlock()
			if !seedStillAddressable {
				commit.ResolvedFrom = current.seedID
				evictedPredecessors++
			}
		} else {
			items := append(parent.Request.Items(), parent.Response.Items()...)
			items = append(items, message(b, strings.Repeat("x", padding)))
			record.Request = parent.Request.WithItems(items)
			commit.ResolvedFrom = parent.Response.Response().SwobuID
		}
		record.Response = responseWithText(nil, id, fmt.Sprintf("answer-%d", index))
		commit.Checkpoint = record
		workspace := fmt.Sprintf("workspace-%d", laneIndex%4)
		before := len(store.records)
		if err := store.Put(ctx, workspace, commit); err != nil {
			b.Fatal(err)
		}
		if len(store.records) <= before {
			evictions++
		}
		if branch && !commit.ResolvedFrom.IsZero() {
			branches++
		}
		if branch {
			current.previous = Checkpoint{}
		} else {
			current.previous = current.head
		}
		current.head = record
		current.depth++
		probes[laneIndex].Store(&readProbe{workspace: workspace, id: id, history: *record.History})
		_, _, _ = store.Get(ctx, workspace, id)
		_, _, _ = store.FindByHistory(ctx, workspace, *record.History)
	}
	b.StopTimer()
	b.ReportMetric(float64(branches), "branches")
	b.ReportMetric(float64(evictions), "evictions")
	b.ReportMetric(float64(evictedPredecessors), "evicted_predecessors")
	b.ReportMetric(float64(freshRoots), "fresh_roots")
	if b.N >= 1000 && (branches == 0 || evictions == 0 || evictedPredecessors == 0) {
		b.Fatalf("mixed workload coverage branches=%d evictions=%d evicted_predecessors=%d", branches, evictions, evictedPredecessors)
	}
}

func benchmarkToolPrelude(b testing.TB, descriptionBytes int) canonical.CanonicalItem {
	b.Helper()
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "large_catalog")
	schema := canonicaltest.Schema(b, `{"type":"object","description":"`+strings.Repeat("s", descriptionBytes)+`"}`)
	tool := canonicaltest.MustFunctionTool(key, strings.Repeat("d", descriptionBytes), schema, canonical.Unspecified[bool]())
	return canonicaltest.ToolDeclarations(b, tool)
}

func historyForBenchmark(value string) *historyfingerprint.History {
	q, _ := historyfingerprint.FingerprintRequest("responses/v1", []byte("q:"+value))
	a, _ := historyfingerprint.FingerprintResponse("responses/v1", []byte("a:"+value))
	history, _ := historyfingerprint.Advance(nil, q, a)
	return &history
}

func segmentSet(tail *historySegment) map[*historySegment]struct{} {
	result := make(map[*historySegment]struct{})
	for segment := tail; segment != nil; segment = segment.parent {
		result[segment] = struct{}{}
	}
	return result
}

func childRecord(t *testing.T, parent Checkpoint, id canonical.SwobuResponseID, text string) Checkpoint {
	child := storeRecord(id, historyFor(t, string(id)))
	child.Request = parent.Request.WithItems(append(append(parent.Request.Items(), parent.Response.Items()...), message(t, text)))
	child.Response = responseWithText(t, id, "answer "+text)
	return child
}

func mustPut(t *testing.T, store Store, workspace string, commit Commit) {
	t.Helper()
	if err := store.Put(context.Background(), workspace, commit); err != nil {
		t.Fatal(err)
	}
}

func storeRecord(id canonical.SwobuResponseID, history *historyfingerprint.History) Checkpoint {
	return Checkpoint{History: history, ExecutionAffinity: affinity(string(id)), Response: responseWithText(nil, id, "")}
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
	history, err := historyfingerprint.Advance(nil, q, a)
	if err != nil {
		t.Fatal(err)
	}
	return &history
}

func message(t testing.TB, text string) canonical.CanonicalItem {
	item, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func requestDirective(t testing.TB, text string) canonical.CanonicalItem {
	item, err := canonical.NewScopedMessageItem(canonical.MessageRoleDeveloper, []canonical.MessagePart{canonical.NewTextMessagePart(text)}, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func requestWithText(t testing.TB, text string) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message(t, text)}})
}

func responseWithText(t *testing.T, id canonical.SwobuResponseID, text string) canonical.CanonicalResponse {
	var items []canonical.CanonicalItem
	if text != "" {
		item, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
		if err != nil {
			if t != nil {
				t.Fatal(err)
			}
			panic(err)
		}
		items = []canonical.CanonicalItem{item}
	}
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "m", items, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		panic(err)
	}
	return response
}
