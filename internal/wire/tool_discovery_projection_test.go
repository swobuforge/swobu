package wire

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestPrepareStaticToolSetRetainsActivatedDeclarations(t *testing.T) {
	discovery, call, result, loaded := staticDiscoveryFixture(t)
	declarationItem, err := canonical.NewToolDeclarationsItem(
		mustToolSet(t, discovery), canonical.ContextScopeHistory,
	)
	if err != nil {
		t.Fatal(err)
	}
	message, _ := canonical.NewMessageItem(
		canonical.MessageRoleUser,
		[]canonical.MessagePart{canonical.NewTextMessagePart("continue")},
	)
	projection, err := PrepareStaticToolSet(
		[]canonical.CanonicalItem{declarationItem, call, result, message},
		[]canonical.ToolDeclaration{discovery, loaded},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Items) != 2 || projection.Items[0].Kind() != canonical.ItemKindToolDeclarations ||
		projection.Items[1].Kind() != canonical.ItemKindMessage ||
		len(projection.Declarations) != 1 || projection.Declarations[0].Key() != loaded.Key() {
		t.Fatalf("static discovery projection = %#v", projection)
	}
	if projection.RemovedEffects != 1 || projection.RemovedDeclarations != 1 {
		t.Fatalf("projection evidence counts = %#v", projection)
	}
}

func TestPrepareStaticToolSetRetainsUnresolvedCall(t *testing.T) {
	discovery, call, _, loaded := staticDiscoveryFixture(t)
	projection, err := PrepareStaticToolSet(
		[]canonical.CanonicalItem{call},
		[]canonical.ToolDeclaration{discovery, loaded},
	)
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("error = %T %v, want target incompatibility", err, err)
	}
	if len(projection.Items) != 0 || len(projection.Declarations) != 0 {
		t.Fatalf("failed projection leaked output = %#v", projection)
	}
}

func TestPrepareStaticToolSetRetainsOrphanResult(t *testing.T) {
	discovery, _, result, loaded := staticDiscoveryFixture(t)
	projection, err := PrepareStaticToolSet(
		[]canonical.CanonicalItem{result},
		[]canonical.ToolDeclaration{discovery, loaded},
	)
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("error = %T %v, want target incompatibility", err, err)
	}
	if len(projection.Items) != 0 || len(projection.Declarations) != 0 {
		t.Fatalf("failed projection leaked output = %#v", projection)
	}
}

func TestPrepareStaticToolSetRejectsLiveDeclaration(t *testing.T) {
	discovery, _, _, _ := staticDiscoveryFixture(t)
	projection, err := PrepareStaticToolSet(
		nil,
		[]canonical.ToolDeclaration{discovery},
	)
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("error = %T %v, want target incompatibility", err, err)
	}
	if len(projection.Items) != 0 || len(projection.Declarations) != 0 {
		t.Fatalf("failed projection leaked output = %#v", projection)
	}
}

func TestPrepareStaticToolSetPairsReusedIDByOccurrence(t *testing.T) {
	discovery, firstCall, firstResult, loaded := staticDiscoveryFixture(t)
	declarationItem, err := canonical.NewToolDeclarationsItem(
		mustToolSet(t, discovery), canonical.ContextScopeHistory,
	)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("search_1")
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	secondCall, _ := canonical.NewToolDiscoveryCallItem(
		callID, canonical.NewJSONObjectToolInput(input), canonical.DiscoveryExecutorClient,
	)
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{loaded})
	secondResult, _ := canonical.NewToolDiscoveryResultItem(
		callID, set, canonical.DiscoveryExecutorClient,
	)

	projection, err := PrepareStaticToolSet(
		[]canonical.CanonicalItem{declarationItem, firstCall, firstResult, secondCall, secondResult},
		[]canonical.ToolDeclaration{discovery, loaded},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Items) != 1 || projection.Items[0].Kind() != canonical.ItemKindToolDeclarations {
		t.Fatalf("reused completed lifecycle projection = %#v", projection.Items)
	}
	if len(projection.Declarations) != 1 || projection.Declarations[0].Key() != loaded.Key() ||
		projection.RemovedEffects != 2 || projection.RemovedDeclarations != 1 {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestPrepareStaticToolSetRejectsRequestScopedDeclarationAfterCompletedPair(t *testing.T) {
	discovery, call, result, loaded := staticDiscoveryFixture(t)
	declarationItem, err := canonical.NewToolDeclarationsItem(
		mustToolSet(t, discovery), canonical.ContextScopeRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PrepareStaticToolSet(
		[]canonical.CanonicalItem{declarationItem, call, result},
		[]canonical.ToolDeclaration{discovery, loaded},
	)
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("error = %T %v, want target incompatibility", err, err)
	}
}

func TestValidateMaterializedRequestRejectsInterleavedDuplicatePendingDiscoveryID(t *testing.T) {
	discovery, call, _, loaded := staticDiscoveryFixture(t)
	declarations, err := canonical.NewToolDeclarationsItem(
		mustToolSet(t, discovery, loaded), canonical.ContextScopeHistory,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			declarations,
			call,
			call,
		},
	})
	err = canonical.ValidateMaterializedRequest(request)
	if err == nil {
		t.Fatal("duplicate pending discovery ID was accepted")
	}
}

func mustToolSet(t *testing.T, declarations ...canonical.ToolDeclaration) canonical.ToolSet {
	t.Helper()
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func staticDiscoveryFixture(t *testing.T) (
	canonical.ToolDeclaration,
	canonical.CanonicalItem,
	canonical.CanonicalItem,
	canonical.ToolDeclaration,
) {
	t.Helper()
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(schemaObject)
	discovery, err := canonical.NewToolDiscoveryTool(
		"find tools", schema, canonical.DiscoveryExecutorProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "read_file")
	loaded, err := canonical.NewFunctionTool(
		key, "", schema, canonical.Unspecified[bool](),
	)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{loaded})
	callID, _ := canonical.NewToolCallID("search_1")
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolDiscoveryCallItem(
		callID, canonical.NewJSONObjectToolInput(input), canonical.DiscoveryExecutorProvider,
	)
	result, _ := canonical.NewToolDiscoveryResultItem(
		callID, set, canonical.DiscoveryExecutorProvider,
	)
	return discovery, call, result, loaded
}
