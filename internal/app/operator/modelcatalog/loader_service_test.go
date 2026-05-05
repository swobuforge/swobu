package modelcatalog

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/ports"
)

type endpointListerStub struct {
	endpoints []endpointintent.Endpoint
}

func (s endpointListerStub) ListEndpoints(ctx context.Context) ([]endpointintent.Endpoint, error) {
	_ = ctx
	return append([]endpointintent.Endpoint(nil), s.endpoints...), nil
}

type providerCatalogSequenceStub struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
}

func (s *providerCatalogSequenceStub) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	_ = target
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		select {
		case s.started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []string{"m2"}, nil
}

type providerCatalogBlockingStub struct {
	calls atomic.Int32
}

func (s *providerCatalogBlockingStub) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	_ = target
	s.calls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

func buildEndpointForModelCatalogTest(t *testing.T) endpointintent.Endpoint {
	t.Helper()
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("p1")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai")
	if err != nil {
		t.Fatalf("ParseProviderSpec: %v", err)
	}
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://api.openai.com/v1", "env:OPENAI_API_KEY", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig: %v", err)
	}
	ep, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint: %v", err)
	}
	return ep
}

func TestLoader_Load_SupersedesPreviousRun(t *testing.T) {
	endpoint := buildEndpointForModelCatalogTest(t)
	providers := &providerCatalogSequenceStub{started: make(chan struct{}, 1)}
	loader := NewLoader(endpointListerStub{endpoints: []endpointintent.Endpoint{endpoint}}, providers)

	result1Ch := make(chan Snapshot, 1)
	err1Ch := make(chan error, 1)
	go func() {
		snap, err := loader.Load(context.Background())
		result1Ch <- snap
		err1Ch <- err
	}()

	select {
	case <-providers.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first load did not start provider probe")
	}

	snap2, err2 := loader.Load(context.Background())
	if err2 != nil {
		t.Fatalf("second load error: %v", err2)
	}
	if len(snap2.Entries) != 1 {
		t.Fatalf("second load entries len = %d, want 1", len(snap2.Entries))
	}
	if snap2.Entries[0].Error != "" {
		t.Fatalf("second load error = %q, want empty", snap2.Entries[0].Error)
	}
	if len(snap2.Entries[0].ModelIDs) != 1 || snap2.Entries[0].ModelIDs[0] != "m2" {
		t.Fatalf("second load model ids = %v, want [m2]", snap2.Entries[0].ModelIDs)
	}

	var snap1 Snapshot
	select {
	case snap1 = <-result1Ch:
	case <-time.After(2 * time.Second):
		t.Fatal("first load did not complete after supersession")
	}
	if err1 := <-err1Ch; err1 != nil {
		t.Fatalf("first load returned error: %v", err1)
	}
	if len(snap1.Entries) != 1 {
		t.Fatalf("first load entries len = %d, want 1", len(snap1.Entries))
	}
	if !strings.Contains(strings.ToLower(snap1.Entries[0].Error), "superseded") {
		t.Fatalf("first load error = %q, want superseded message", snap1.Entries[0].Error)
	}
}

func TestLoader_Load_EntryTimeoutDoesNotFailWholeSnapshot(t *testing.T) {
	endpoint := buildEndpointForModelCatalogTest(t)
	providers := &providerCatalogBlockingStub{}
	loader := NewLoader(endpointListerStub{endpoints: []endpointintent.Endpoint{endpoint}}, providers)

	old := modelCatalogProbeTimeout
	modelCatalogProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		modelCatalogProbeTimeout = old
	})

	snap, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(snap.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(snap.Entries))
	}
	if got := snap.Entries[0].Error; got != "model catalog probe timed out" {
		t.Fatalf("entry error = %q, want %q", got, "model catalog probe timed out")
	}
}
