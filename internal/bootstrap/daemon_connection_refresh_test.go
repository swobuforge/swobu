package bootstrap

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	credentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestPublishRefreshedConnectionStagesRevisionAndPreservesCapturedSlot(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	registry, err := connectionfixture.Open(path, profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse("cn_refresh")
	original, err := credentials.StoreConnectionCredential(id.String(), "synthetic-old", credentials.CredentialWritePolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := routing.NewStandardConnection("chatgpt", "", original)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Refresh fixture", spec, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	target, err := exchange.ProviderTargetFromModelConnection("target", value, "responses_stream")
	if err != nil {
		t.Fatal(err)
	}
	if err := publishRefreshedConnection(t.Context(), registry, target, "synthetic-new", credentials.CredentialWritePolicyFile); err != nil {
		t.Fatal(err)
	}
	reopened, err := connectionfixture.Open(path, profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := current.CredentialBinding("")
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision().String() != "2" || binding.String() == original {
		t.Fatal("no new immutable revision")
	}
	stored, err := credentials.ResolveConnectionStoredBundle(id.String(), binding.String())
	bundle, ok, decodeErr := credentials.DecodeTokenBundle(stored)
	if err != nil || decodeErr != nil || !ok || bundle.AccessToken != "synthetic-new" {
		t.Fatal("new material missing", err)
	}
	old, err := credentials.ResolveConnectionStoredBundle(id.String(), original)
	oldBundle, oldOK, decodeErr := credentials.DecodeTokenBundle(old)
	if err != nil || decodeErr != nil || !oldOK || oldBundle.AccessToken != "synthetic-old" {
		t.Fatal("captured slot changed", err)
	}
	stages := len(reopened.StagedCredentials())
	if stages < 2 {
		t.Fatal("cleanup inventory missing backend candidates")
	}
	if err := publishRefreshedConnection(t.Context(), reopened, target, "synthetic-loser", credentials.CredentialWritePolicyFile); !errors.Is(err, connections.ErrRevisionConflict) {
		t.Fatal("stale refresh published", err)
	}
	if len(reopened.StagedCredentials()) != stages {
		t.Fatal("known conflict allocated new material")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	next, err := exchange.ProviderTargetFromModelConnection("target", current, "responses_stream")
	if err != nil {
		t.Fatal(err)
	}
	if err := publishRefreshedConnection(ctx, reopened, next, "synthetic-cancelled", credentials.CredentialWritePolicyFile); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled refresh published", err)
	}
	if _, err := reopened.ReplaceCredentialBinding(t.Context(), id, current.Revision(), "", credentialref.Parse(original), binding, time.Now()); err == nil {
		t.Fatal("old locator was accepted after replacement")
	}
	// Two attempts refreshing the same revision have exactly one publication
	// winner. Any losing staged slot remains in the durable cleanup inventory.
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, material := range []string{"synthetic-concurrent-a", "synthetic-concurrent-b"} {
		go func(material string) {
			<-start
			results <- publishRefreshedConnection(t.Context(), reopened, next, material, credentials.CredentialWritePolicyFile)
		}(material)
	}
	close(start)
	wins, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			wins++
		} else if errors.Is(err, connections.ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
	final, err := reopened.Get(id)
	if err != nil || final.Revision().String() != "3" {
		t.Fatal("concurrent refresh lost revision CAS", err)
	}

}
